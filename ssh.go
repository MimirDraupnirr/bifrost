package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSHConfig : la seedbox. Le mot de passe n'y figure jamais, il vit en
// mémoire le temps de la session (docs/23 §4).
type SSHConfig struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	KeyPath string `json:"key_path"`
	DataDir string `json:"data_dir"`
	// Clé d'hôte acceptée (base64), si elle n'est pas dans ~/.ssh/known_hosts.
	HostKey string `json:"host_key,omitempty"`
}

const agentPath = `"$HOME/.bifrost/bifrost"`

// hostKeyError : hôte inconnu (ou clé changée) — la page montre l'empreinte
// et le membre accepte explicitement.
type hostKeyError struct {
	Host, Fingerprint, Key string
	Changed                bool
}

func (e *hostKeyError) Error() string {
	if e.Changed {
		return "la clé d'hôte de " + e.Host + " a CHANGÉ (" + e.Fingerprint + ") : vérifie avant d'accepter"
	}
	return "hôte inconnu : " + e.Host + " (" + e.Fingerprint + ")"
}

func userHome() (string, error) {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h, nil
	}
	// Lancé sans HOME (service, outil de prévisualisation) : le compte le sait.
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.HomeDir, nil
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := userHome(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func authMethods(cfg *SSHConfig, password string) ([]ssh.AuthMethod, []string) {
	var methods []ssh.AuthMethod
	var notes []string
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			ag := sshagent.NewClient(conn)
			if keys, err := ag.List(); err == nil && len(keys) > 0 {
				methods = append(methods, ssh.PublicKeysCallback(ag.Signers))
				notes = append(notes, fmt.Sprintf("agent SSH (%d clés)", len(keys)))
			}
		}
	}
	candidates := []string{cfg.KeyPath}
	if cfg.KeyPath == "" {
		candidates = []string{"~/.ssh/id_ed25519", "~/.ssh/id_ecdsa", "~/.ssh/id_rsa"}
	}
	for _, c := range candidates {
		pem, err := os.ReadFile(expandHome(c))
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(pem)
		if err != nil && password != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(password))
		}
		if err == nil {
			methods = append(methods, ssh.PublicKeys(signer))
			notes = append(notes, "clé "+c)
		}
	}
	if password != "" {
		methods = append(methods, ssh.Password(password))
		notes = append(notes, "mot de passe")
	}
	return methods, notes
}

func dialSSH(cfg *SSHConfig, password string) (*ssh.Client, error) {
	if cfg.Host == "" || cfg.User == "" {
		return nil, errors.New("hôte et utilisateur SSH requis")
	}
	methods, notes := authMethods(cfg, password)
	if len(methods) == 0 {
		return nil, errors.New("aucun moyen d'authentification : clé SSH introuvable et pas de mot de passe")
	}
	var hkErr *hostKeyError
	callback := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		marshaled := base64.StdEncoding.EncodeToString(key.Marshal())
		if cfg.HostKey != "" {
			if cfg.HostKey == marshaled {
				return nil
			}
			hkErr = &hostKeyError{Host: hostname, Fingerprint: ssh.FingerprintSHA256(key), Key: marshaled, Changed: true}
			return hkErr
		}
		if home, err := userHome(); err == nil {
			if kh, err := knownhosts.New(filepath.Join(home, ".ssh", "known_hosts")); err == nil && kh(hostname, remote, key) == nil {
				return nil
			}
		}
		hkErr = &hostKeyError{Host: hostname, Fingerprint: ssh.FingerprintSHA256(key), Key: marshaled}
		return hkErr
	}
	port := cfg.Port
	if port == 0 {
		port = 22
	}
	client, err := ssh.Dial("tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(port)), &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            methods,
		HostKeyCallback: callback,
		Timeout:         15 * time.Second,
	})
	if err != nil {
		if hkErr != nil {
			return nil, hkErr
		}
		return nil, fmt.Errorf("ssh : %w (moyens essayés : %s)", err, strings.Join(notes, ", "))
	}
	return client, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// runSSH exécute une commande ; stderr est livré ligne à ligne à onStderr
// (progression de l'agent), stdout est renvoyé entier.
func runSSH(ctx context.Context, client *ssh.Client, cmd string, stdin io.Reader, onStderr func(string)) ([]byte, error) {
	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	sess.Stdin = stdin
	stderrPipe, err := sess.StderrPipe()
	if err != nil {
		return nil, err
	}
	var stdout bytes.Buffer
	sess.Stdout = &stdout
	var tail []string
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(stderrPipe)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if onStderr != nil {
				onStderr(line)
			}
			if !strings.HasPrefix(line, "progress ") {
				tail = append(tail, line)
				if len(tail) > 5 {
					tail = tail[1:]
				}
			}
		}
		close(done)
	}()
	if err := sess.Start(cmd); err != nil {
		return nil, err
	}
	waitErr := make(chan error, 1)
	go func() { waitErr <- sess.Wait() }()
	select {
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		return nil, ctx.Err()
	case err := <-waitErr:
		<-done
		if err != nil {
			msg := strings.TrimSpace(strings.Join(tail, " · "))
			if msg == "" {
				msg = err.Error()
			}
			return stdout.Bytes(), errors.New(msg)
		}
		return stdout.Bytes(), nil
	}
}

// ---- l'agent : le binaire Linux de Bifröst, copié dans ~/.bifrost ----

type testStep struct {
	Label  string `json:"label"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

func embeddedAgent(arch string) ([]byte, error) {
	data, err := agentFS.ReadFile("agent/bifrost-linux-" + arch)
	if err == nil && len(data) > 0 {
		return data, nil
	}
	// Build de dev sans agents embarqués : si ce poste est lui-même un
	// Linux de même architecture (Docker sur la seedbox), il se copie.
	if runtime.GOOS == "linux" && runtime.GOARCH == arch {
		exe, err := os.Executable()
		if err == nil {
			return os.ReadFile(exe)
		}
	}
	return nil, fmt.Errorf("agent linux/%s non embarqué dans ce build (lance `make build`)", arch)
}

// deployAgent vérifie la seedbox et y pose l'agent si sa version diffère.
func deployAgent(ctx context.Context, client *ssh.Client, dataDir string) []testStep {
	var steps []testStep
	add := func(label string, ok bool, detail string) { steps = append(steps, testStep{label, ok, detail}) }

	out, err := runSSH(ctx, client, "uname -s -m", nil, nil)
	if err != nil {
		add("Connexion SSH", false, err.Error())
		return steps
	}
	uname := strings.Fields(string(out))
	if len(uname) != 2 || uname[0] != "Linux" {
		add("Connexion SSH", false, "la seedbox n'est pas un Linux : "+strings.TrimSpace(string(out)))
		return steps
	}
	arch := map[string]string{"x86_64": "amd64", "aarch64": "arm64", "arm64": "arm64"}[uname[1]]
	add("Connexion SSH", true, "Linux "+uname[1])

	out, _ = runSSH(ctx, client, "command -v mediainfo >/dev/null && mediainfo --Version | head -n 2 | tail -n 1", nil, nil)
	if v := strings.TrimSpace(string(out)); v != "" {
		add("mediainfo", true, v)
	} else {
		add("mediainfo", false, "absent — installe-le (sudo apt install mediainfo) ; sans lui les facettes sont déclarées à la main")
	}

	if arch == "" {
		add("Agent", false, "architecture non prise en charge : "+uname[1])
		return steps
	}
	pushed, detail, err := pushAgent(ctx, client, arch)
	if err != nil {
		add("Agent", false, err.Error())
		return steps
	}
	if pushed {
		add("Agent", true, "déployé dans ~/.bifrost ("+detail+")")
	} else {
		add("Agent", true, "déjà en place ("+detail+")")
	}

	if dataDir != "" {
		out, err = runSSH(ctx, client, agentPath+" agent ls "+shellQuote(dataDir), nil, nil)
		var entries []DirEntry
		if err == nil {
			err = json.Unmarshal(out, &entries)
		}
		if err != nil {
			add("Dossier des données", false, err.Error())
		} else {
			add("Dossier des données", true, fmt.Sprintf("%d éléments dans %s", len(entries), dataDir))
		}
	}
	return steps
}

// pushAgent : compare la version de l'agent distant à celle du poste et le
// re-copie si elle diffère. Un build « dev » est toujours re-copié : sa
// version ne dit rien de son contenu.
func pushAgent(ctx context.Context, client *ssh.Client, arch string) (pushed bool, detail string, err error) {
	out, _ := runSSH(ctx, client, agentPath+" agent version 2>/dev/null || true", nil, nil)
	remote := strings.TrimSpace(string(out))
	if remote == Version && Version != "dev" {
		return false, remote, nil
	}
	bin, err := embeddedAgent(arch)
	if err != nil {
		return false, "", err
	}
	cmd := `mkdir -p "$HOME/.bifrost" && cat > "$HOME/.bifrost/bifrost.tmp" && chmod 755 "$HOME/.bifrost/bifrost.tmp" && mv -f "$HOME/.bifrost/bifrost.tmp" "$HOME/.bifrost/bifrost" && ` + agentPath + ` agent version`
	out, err = runSSH(ctx, client, cmd, bytes.NewReader(bin), nil)
	if err != nil {
		return false, "", err
	}
	return true, fmt.Sprintf("%s, %.1f Mo", strings.TrimSpace(string(out)), float64(len(bin))/1e6), nil
}

// ensureAgent : une fois par lancement, avant la première commande distante,
// l'agent est aligné sur le poste. Sinon une nouvelle sous-commande (check…)
// échouait sur une seedbox dont l'agent datait d'un test précédent.
var agentChecked sync.Map // host → true

func ensureAgent(ctx context.Context, r *remoteSource, client *ssh.Client) error {
	if _, done := agentChecked.Load(r.cfg.Host); done {
		return nil
	}
	out, err := runSSH(ctx, client, "uname -m", nil, nil)
	if err != nil {
		return err
	}
	arch := map[string]string{"x86_64": "amd64", "aarch64": "arm64", "arm64": "arm64"}[strings.TrimSpace(string(out))]
	if arch == "" {
		return errors.New("architecture de la seedbox non prise en charge : " + strings.TrimSpace(string(out)))
	}
	if _, _, err := pushAgent(ctx, client, arch); err != nil {
		return err
	}
	agentChecked.Store(r.cfg.Host, true)
	return nil
}

// ---- la seedbox comme source de fichiers ----

type remoteSource struct {
	cfg      SSHConfig
	password string
}

func (r *remoteSource) client() (*ssh.Client, error) {
	c, err := dialSSH(&r.cfg, r.password)
	if err != nil {
		return nil, err
	}
	if err := ensureAgent(context.Background(), r, c); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (r *remoteSource) Home() string {
	if r.cfg.DataDir != "" {
		return r.cfg.DataDir
	}
	return "."
}

func (r *remoteSource) List(ctx context.Context, p string) ([]DirEntry, error) {
	c, err := r.client()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return r.ls(ctx, c, p, "")
}

func (r *remoteSource) Dirs(ctx context.Context, p string) ([]DirEntry, error) {
	c, err := r.client()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return r.ls(ctx, c, p, "--dirs ")
}

// ls : « agent ls » sur la seedbox ; flags vide ou « --dirs » (sans taille des dossiers).
func (r *remoteSource) ls(ctx context.Context, c *ssh.Client, p, flags string) ([]DirEntry, error) {
	if p == "" {
		p = r.Home()
	}
	out, err := runSSH(ctx, c, agentPath+" agent ls "+flags+shellQuote(p), nil, nil)
	if err != nil {
		return nil, err
	}
	var entries []DirEntry
	return entries, json.Unmarshal(out, &entries)
}

func (r *remoteSource) MakeTorrent(ctx context.Context, p, source string, progress progressFunc) ([]byte, error) {
	c, err := r.client()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	raw, err := runSSH(ctx, c, agentPath+" agent mktorrent --source "+shellQuote(source)+" "+shellQuote(p), nil, func(line string) {
		var done, total int64
		if _, err := fmt.Sscanf(line, "progress %d/%d", &done, &total); err == nil && progress != nil {
			progress(done, total)
		}
	})
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, errors.New("l'agent n'a rien renvoyé")
	}
	return raw, nil
}

func (r *remoteSource) MediaInfo(ctx context.Context, p string) (string, error) {
	c, err := r.client()
	if err != nil {
		return "", err
	}
	defer c.Close()
	out, err := runSSH(ctx, c, agentPath+" agent mediainfo "+shellQuote(p), nil, nil)
	return string(out), err
}

// MainFile : la seedbox est un Linux, séparateur « / » quel que soit le poste.
func (r *remoteSource) MainFile(root string, t *Torrent) string {
	var best TorrentFile
	for _, f := range t.Files {
		if f.Size > best.Size {
			best = f
		}
	}
	if best.Path == "" {
		return ""
	}
	if len(t.Files) == 1 && best.Path == t.Name && path.Base(root) == t.Name {
		return root // fichier seul : le chemin choisi est déjà le fichier
	}
	return path.Join(root, best.Path)
}
