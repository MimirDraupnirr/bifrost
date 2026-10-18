// Bifröst, application de bureau : un écran de lancement qui met l'application à jour
// (façon Discord), puis le serveur Go en sidecar (`bifrost --managed`) affiché dans une
// vraie fenêtre. Fermer la fenêtre pendant un lot la range dans la barre des tâches ;
// sans lot, l'application se ferme. Le serveur reste le même que celui du binaire seul
// et de l'image Docker : rien de son interface n'est dupliqué ici.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Mutex;

use tauri::menu::{Menu, MenuItem};
use tauri::tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent};
use tauri::{AppHandle, Manager, RunEvent, Url, WebviewUrl, WebviewWindowBuilder, WindowEvent};
use tauri_plugin_opener::OpenerExt;
use tauri_plugin_shell::process::{CommandChild, CommandEvent};
use tauri_plugin_shell::ShellExt;
use tauri_plugin_updater::UpdaterExt;

/// Ce que l'application suit du serveur Go.
#[derive(Default)]
struct Core {
    child: Mutex<Option<CommandChild>>,
    /// Un lot tourne (lignes BIFROST_BATCH du serveur).
    batch: AtomicBool,
    /// Sortie demandée : la fin du serveur n'est alors pas une panne.
    quitting: AtomicBool,
}

/// Met à jour l'écran de lancement : un texte, et une progression (0..1) ou rien
/// (barre indéterminée). Passe par eval : la page n'appelle jamais Rust.
fn splash(app: &AppHandle, text: &str, progress: Option<f64>, error: bool) {
    if let Some(w) = app.get_webview_window("splash") {
        let p = progress.map_or("null".to_string(), |p| format!("{:.3}", p.clamp(0.0, 1.0)));
        let t = serde_json::to_string(text).unwrap_or_default();
        let _ = w.eval(&format!("window.setStep&&setStep({t},{p},{error})"));
    }
}

/// Cherche une mise à jour, la télécharge en montrant la progression, l'installe et
/// relance. Renvoie false quand il n'y a rien à faire ou qu'elle échoue : on démarre
/// alors la version en place (hors ligne, GitHub indisponible…).
async fn update(app: &AppHandle) -> bool {
    if cfg!(debug_assertions) {
        return false; // build de dev en 0.0.0 : toujours « en retard »
    }
    splash(app, "Recherche de mises à jour…", None, false);
    let Ok(updater) = app.updater() else { return false };
    let Ok(Some(up)) = updater.check().await else { return false };
    let label = format!("Mise à jour vers {}", up.version);
    splash(app, &label, Some(0.0), false);
    let (h1, h2) = (app.clone(), app.clone());
    let mut got: u64 = 0;
    let done = up
        .download_and_install(
            move |chunk, total| {
                got += chunk as u64;
                if let Some(total) = total.filter(|t| *t > 0) {
                    splash(&h1, &label, Some(got as f64 / total as f64), false);
                }
            },
            move || splash(&h2, "Installation…", None, false),
        )
        .await;
    match done {
        Ok(()) => app.restart(),
        Err(e) => {
            splash(app, &format!("Mise à jour impossible : {e}"), None, true);
            false
        }
    }
}

/// Lance le serveur Go et suit ses lignes BIFROST_* : READY ouvre la fenêtre,
/// BATCH tient l'état du lot pour la barre des tâches.
fn launch(app: &AppHandle) {
    splash(app, "Démarrage…", None, false);
    let cmd = match app.shell().sidecar("bifrost") {
        Ok(c) => c.args(["--managed", "--no-browser", "--listen", "127.0.0.1:8790"]),
        Err(e) => return splash(app, &format!("Serveur introuvable : {e}"), None, true),
    };
    let (mut rx, child) = match cmd.spawn() {
        Ok(x) => x,
        Err(e) => return splash(app, &format!("Serveur impossible à lancer : {e}"), None, true),
    };
    *app.state::<Core>().child.lock().unwrap() = Some(child);

    let h = app.clone();
    tauri::async_runtime::spawn(async move {
        let mut last_err = String::new();
        while let Some(ev) = rx.recv().await {
            match ev {
                CommandEvent::Stdout(line) => {
                    let line = String::from_utf8_lossy(&line);
                    let line = line.trim();
                    let core = h.state::<Core>();
                    if let Some(url) = line.strip_prefix("BIFROST_READY ") {
                        open_main(&h, url);
                    } else if line == "BIFROST_BATCH 1" {
                        core.batch.store(true, Ordering::SeqCst);
                    } else if line == "BIFROST_BATCH 0" {
                        core.batch.store(false, Ordering::SeqCst);
                        // Fenêtre fermée pendant le lot : il est fini, l'application aussi.
                        let hidden = h
                            .get_webview_window("main")
                            .is_some_and(|w| !w.is_visible().unwrap_or(true));
                        if hidden {
                            quit(&h);
                        }
                    }
                }
                CommandEvent::Stderr(line) => {
                    last_err = String::from_utf8_lossy(&line).trim().to_string();
                }
                CommandEvent::Terminated(p) => {
                    if !h.state::<Core>().quitting.load(Ordering::SeqCst) {
                        crashed(&h, p.code, &last_err);
                    }
                    break;
                }
                _ => {}
            }
        }
    });
}

/// Le serveur s'est arrêté sans qu'on le demande : on le dit sur l'écran de lancement.
fn crashed(app: &AppHandle, code: Option<i32>, last_err: &str) {
    if let Some(w) = app.get_webview_window("main") {
        let _ = w.destroy();
    }
    let text = match (code, last_err.is_empty()) {
        (_, false) => format!("Le serveur s'est arrêté : {last_err}"),
        (Some(c), true) => format!("Le serveur s'est arrêté (code {c})."),
        (None, true) => "Le serveur s'est arrêté.".to_string(),
    };
    if let Some(w) = app.get_webview_window("splash") {
        let _ = w.show();
        let _ = w.set_focus();
    }
    splash(app, &text, None, true);
}

/// La fenêtre principale : la page du serveur Go. Les liens qui en sortent
/// (Draupnirr, GitHub…) s'ouvrent dans le navigateur, pas dans l'application.
fn open_main(app: &AppHandle, url: &str) {
    if app.get_webview_window("main").is_some() {
        return show_main(app);
    }
    let Ok(url) = url.parse::<Url>() else { return };
    let origin = url.origin();
    let (h1, h2) = (app.clone(), app.clone());
    let built = WebviewWindowBuilder::new(app, "main", WebviewUrl::External(url))
        .title("Bifröst")
        .inner_size(1280.0, 860.0)
        .min_inner_size(420.0, 520.0)
        .center()
        .on_navigation(move |u| {
            if u.origin() == origin {
                return true;
            }
            let _ = h1.opener().open_url(u.as_str(), None::<&str>);
            false
        })
        .on_new_window(move |u, _| {
            let _ = h2.opener().open_url(u.as_str(), None::<&str>);
            tauri::webview::NewWindowResponse::Deny
        })
        .build();
    // Caché, pas détruit : il revient pour dire une panne du serveur.
    if built.is_ok() {
        if let Some(s) = app.get_webview_window("splash") {
            let _ = s.hide();
        }
    }
}

fn show_main(app: &AppHandle) {
    if let Some(w) = app.get_webview_window("main") {
        let _ = w.show();
        let _ = w.unminimize();
        let _ = w.set_focus();
    }
}

/// Sortie voulue : le serveur est arrêté avec l'application (un lot en cours s'arrête
/// comme avec le bouton Arrêter : la release en cours n'est pas comptée comme décision).
fn quit(app: &AppHandle) {
    stop_server(app);
    app.exit(0);
}

fn stop_server(app: &AppHandle) {
    let core = app.state::<Core>();
    core.quitting.store(true, Ordering::SeqCst);
    let child = core.child.lock().unwrap().take();
    if let Some(child) = child {
        let _ = child.kill();
    }
}

fn tray(app: &AppHandle) -> tauri::Result<()> {
    let open = MenuItem::with_id(app, "open", "Ouvrir Bifröst", true, None::<&str>)?;
    let exit = MenuItem::with_id(app, "quit", "Quitter", true, None::<&str>)?;
    let menu = Menu::with_items(app, &[&open, &exit])?;
    TrayIconBuilder::with_id("main")
        .icon(app.default_window_icon().cloned().expect("icône de l'application"))
        .tooltip("Bifröst")
        .menu(&menu)
        .show_menu_on_left_click(false)
        .on_menu_event(|app, ev| match ev.id().as_ref() {
            "open" => show_main(app),
            "quit" => quit(app),
            _ => {}
        })
        .on_tray_icon_event(|tray, ev| {
            if let TrayIconEvent::Click { button: MouseButton::Left, button_state: MouseButtonState::Up, .. } = ev {
                show_main(tray.app_handle());
            }
        })
        .build(app)?;
    Ok(())
}

fn main() {
    tauri::Builder::default()
        // Un second lancement (raccourci, double-clic) ramène la fenêtre existante.
        .plugin(tauri_plugin_single_instance::init(|app, _, _| show_main(app)))
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_updater::Builder::new().build())
        .manage(Core::default())
        .setup(|app| {
            tray(app.handle())?;
            let h = app.handle().clone();
            tauri::async_runtime::spawn(async move {
                if !update(&h).await {
                    launch(&h);
                }
            });
            Ok(())
        })
        .on_window_event(|w, ev| {
            // Pendant un lot, fermer range la fenêtre : le lot continue, l'icône reste.
            // Sinon on quitte (l'écran de lancement caché garderait l'application en vie).
            if let WindowEvent::CloseRequested { api, .. } = ev {
                if w.label() == "main" {
                    api.prevent_close();
                    if w.state::<Core>().batch.load(Ordering::SeqCst) {
                        let _ = w.hide();
                    } else {
                        quit(w.app_handle());
                    }
                }
            }
        })
        .build(tauri::generate_context!())
        .expect("démarrage de Bifröst")
        .run(|app, ev| match ev {
            // Plus de fenêtre (fermée hors lot, ou écran de lancement fermé) : on sort proprement.
            RunEvent::ExitRequested { .. } => stop_server(app),
            #[cfg(target_os = "macos")]
            RunEvent::Reopen { .. } => show_main(app),
            _ => {}
        });
}
