# Bifröst

Outil d'upload pour Draupnirr : un binaire, une page locale, zéro installation.
Il prépare une release (fichiers, `.torrent`, MediaInfo, œuvre TMDB, fiche
technique calculée par Draupnirr, modèle de présentation) et la publie par
l'API avec un **jeton API** créé dans *Profil › Sécurité › Jetons API*.

```bash
go build -o bifrost . && ./bifrost          # ouvre http://127.0.0.1:8790
./bifrost --listen 0.0.0.0:8790             # hors loopback : mot de passe local exigé (à venir)
./bifrost agent mktorrent --source DRAUPNIRR /chemin/release > release.torrent
./bifrost agent mediainfo /chemin/release/film.mkv
./bifrost agent ls /chemin
```

Conception et décisions : `ygrasil/docs/23-BIFROST-OUTIL-UPLOAD-GO.md`.

État : **v0.1 en cours** — mode disque local. Seedbox par SSH, clients torrent,
Docker et mise à jour automatique arrivent dans les versions suivantes.
