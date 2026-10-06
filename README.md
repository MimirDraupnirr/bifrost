# Bifröst

Outil d'upload pour Draupnirr : un binaire, une page locale, zéro installation.
Il prépare une release (fichiers, `.torrent`, MediaInfo, œuvre TMDB, fiche
technique calculée par Draupnirr, modèle de présentation), la publie par l'API
avec un **jeton API** (*Profil › Sécurité › Jetons API*), puis remet le
`.torrent` personnalisé dans ton client pour que tu seedes aussitôt.

## Installer

**Binaire** — page *Releases* : macOS (Apple Silicon, Intel), Linux, Windows.
Double-clic ou terminal, le navigateur s'ouvre sur http://127.0.0.1:8790.
Aucun droit administrateur. Le binaire se met à jour tout seul (signature
ed25519 vérifiée), case décochable dans la page.

**Docker**, en général sur la seedbox (voir `docker-compose.example.yml`) :

```bash
docker run -d --name bifrost --restart unless-stopped -p 8790:8790 \
  -v bifrost-config:/config -v /home/moi/downloads:/data:ro \
  ghcr.io/mimirdraupnirr/bifrost:latest
```

Hors loopback (Docker, `--listen 0.0.0.0:8790`), la page exige un **mot de
passe local** défini au premier accès.

## Sources de fichiers et clients

- **Ce poste** : parcours du disque, hachage et `mediainfo` locaux.
- **Seedbox par SSH** : clé ou mot de passe de session (jamais enregistré),
  empreinte d'hôte acceptée explicitement. Bifröst copie son propre binaire
  Linux dans `~/.bifrost` et l'exécute à la demande : pas de démon, pas de port.
- **Clients** : qBittorrent (export du `.torrent` d'origine, sans re-hachage),
  rTorrent/ruTorrent, Transmission, Deluge (re-hachage par la source).
  « Aucun client » garde le `.torrent` dans un dossier.

## Développer

```bash
make test        # vet + tests
make build       # agents Linux (amd64, arm64) puis binaire du poste
./bifrost --no-browser --listen 127.0.0.1:8790 --config /tmp/bifrost.json
./bifrost agent mktorrent --source DRAUPNIRR /chemin/release > release.torrent
./bifrost agent keygen   # paire ed25519 : publique dans update.go, privée en secret BIFROST_SIGNING_KEY
```

Une release = tag `vX.Y.Z` : goreleaser publie les binaires et `checksums.txt`,
le workflow signe `checksums.txt.sig`, et l'image multi-arch part sur ghcr.io.

Conception et décisions : `ygrasil/docs/23-BIFROST-OUTIL-UPLOAD-GO.md`.
