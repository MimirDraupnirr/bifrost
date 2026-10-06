// Bifröst — outil d'upload pour Draupnirr. Un binaire : la page locale pour
// le membre, et les mêmes fonctions en sous-commandes `agent` quand il est
// copié sur une seedbox et piloté par SSH (docs/23 §4).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Version est posée par goreleaser (-X main.Version=…) ; « dev » en local.
var Version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "agent" {
		os.Exit(agent(os.Args[2:]))
	}

	listen := flag.String("listen", "127.0.0.1:8790", "adresse d'écoute de la page locale")
	configPath := flag.String("config", defaultConfigPath(), "fichier de configuration")
	noBrowser := flag.Bool("no-browser", false, "ne pas ouvrir le navigateur")
	showVersion := flag.Bool("version", false, "afficher la version")
	flag.Parse()

	if *showVersion {
		fmt.Println("bifrost", Version, runtime.GOOS+"/"+runtime.GOARCH)
		return
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config :", err)
		os.Exit(1)
	}
	cleanupOldBinary()

	// Mise à jour automatique AVANT de servir : rien ne tourne encore, c'est
	// le seul moment où remplacer le binaire ne dérange personne.
	if cfg.autoUpdate() && !inDocker() && Version != "dev" {
		if rel, err := checkUpdate(context.Background()); err == nil && rel != nil {
			fmt.Println("Mise à jour", Version, "→", rel.Version)
			if err := applyUpdate(context.Background(), rel); err != nil {
				fmt.Fprintln(os.Stderr, "mise à jour refusée :", err)
			}
		}
	}

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "écoute impossible :", err)
		os.Exit(1)
	}
	url := "http://" + ln.Addr().String()
	fmt.Println("Bifröst", Version, "—", url)

	srv := newServer(cfg, *configPath)
	srv.requireAuth = !isLoopback(ln.Addr().String())
	if srv.requireAuth {
		fmt.Println("Écoute hors loopback : mot de passe local exigé (défini au premier accès).")
	}
	if !*noBrowser {
		go openBrowser(url)
	}
	if err := http.Serve(ln, srv); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// agent : les sous-commandes exécutées sur la seedbox par SSH. Sortie utile
// sur stdout (JSON ou .torrent), progression et erreurs sur stderr.
func agent(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage : bifrost agent <mktorrent|mediainfo|ls|version> …")
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Println(Version)
		return 0
	case "keygen":
		return keygen()
	case "verify":
		// bifrost agent verify checksums.txt checksums.txt.sig — contrôle
		// qu'une release est signée par la clé de CE build (support, audit).
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage : bifrost agent verify <checksums.txt> <checksums.txt.sig>")
			return 2
		}
		sums, err1 := os.ReadFile(args[1])
		sig, err2 := os.ReadFile(args[2])
		if err1 != nil || err2 != nil {
			fmt.Fprintln(os.Stderr, "fichiers illisibles")
			return 1
		}
		if err := verifySignature(signingPublicKey, sums, sig); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("signature valide")
		return 0
	case "sign":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage : bifrost agent sign <fichier>")
			return 2
		}
		return sign(args[1])
	case "ls":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage : bifrost agent ls <dossier>")
			return 2
		}
		entries, err := listDir(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return printJSON(entries)
	case "check":
		// bifrost agent check <root> — lit un torrent (JSON de Torrent) sur stdin,
		// répond {"missing":[…]} : l'arborescence est-elle sur ce disque ?
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage : bifrost agent check <dossier>  (torrent JSON sur stdin)")
			return 2
		}
		var t Torrent
		if err := json.NewDecoder(os.Stdin).Decode(&t); err != nil {
			fmt.Fprintln(os.Stderr, "torrent illisible :", err)
			return 1
		}
		return printJSON(map[string]any{"missing": checkFiles(args[1], &t)})
	case "mediainfo":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage : bifrost agent mediainfo <fichier>")
			return 2
		}
		out, err := mediaInfo(context.Background(), args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Print(out)
		return 0
	case "mktorrent":
		fs := flag.NewFlagSet("mktorrent", flag.ContinueOnError)
		source := fs.String("source", "DRAUPNIRR", "tag source de l'info-dict")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 1 {
			fmt.Fprintln(os.Stderr, "usage : bifrost agent mktorrent [--source TAG] <chemin>")
			return 2
		}
		last := time.Now()
		raw, err := makeTorrent(fs.Arg(0), *source, func(done, total int64) {
			if time.Since(last) > time.Second || done == total {
				last = time.Now()
				fmt.Fprintf(os.Stderr, "progress %d/%d\n", done, total)
			}
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		os.Stdout.Write(raw)
		return 0
	}
	fmt.Fprintln(os.Stderr, "sous-commande inconnue :", args[0])
	return 2
}

func printJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func openBrowser(url string) {
	time.Sleep(300 * time.Millisecond)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// isLoopback : hors loopback, la page exigera un mot de passe local (docs/23 §9.2).
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}
