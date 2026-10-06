# Contribuer à Bifröst

Merci de prendre le temps. Bifröst est petit et volontairement simple : un binaire Go sans framework, une page en HTML, CSS et JavaScript natifs. Les contributions qui gardent ce cap sont les bienvenues.

## Avant d'écrire du code

- **Ouvre une issue d'abord** pour tout changement qui n'est pas une correction évidente : ça évite de travailler pour rien si la direction ne convient pas.
- Les règles de nomenclature, les facettes, le NFO et les doublons sont calculés par **Draupnirr**, pas par Bifröst. Une demande qui touche à ça relève du site.
- Une dépendance nouvelle doit se justifier par ce que la bibliothèque standard ne sait pas faire. Aujourd'hui : `golang.org/x/crypto` (SSH, argon2) et rien d'autre.

## Mettre en place

```bash
git clone https://github.com/MimirDraupnirr/bifrost && cd bifrost
make test                     # go vet + go test
make build                    # agents Linux puis binaire du poste
./bifrost --no-browser --listen 127.0.0.1:8790 --config /tmp/bifrost.json
```

Pour tester contre un site, utilise un **jeton API de test** et jamais ton compte principal. Ne commite aucun fichier de configuration, aucun `.torrent`, aucun chemin ou hôte de ta propre seedbox, aucune capture qui les montre.

## Une pull request

1. Branche depuis `main`, un sujet par PR.
2. `gofmt -l .` ne doit rien afficher ; `make test` doit passer. La CI vérifie les deux, plus `govulncheck`.
3. Toute logique non triviale vient avec un test (table-driven, sans framework). Pas de test pour une ligne évidente.
4. Les contrats HTTP locaux `/ui/*` servent la page : si tu en changes un, change la page dans la même PR.
5. Messages de commit en français, à l'impératif ou au présent, qui disent **pourquoi** et pas seulement quoi. Pas de trailer d'outil.
6. Décris dans la PR comment tu as vérifié (quel site, quelle source, quel client), sans coller de secret.

## Style

- Go : `gofmt`, noms courts, erreurs explicites en français pour ce que voit le membre, commentaires qui expliquent une décision ou un piège vécu, pas ce que le code dit déjà.
- Page : pas de bibliothèque, pas de CDN, pas de police distante ; icônes SVG inline, jamais d'emoji ; clair et sombre ; utilisable à 390 px.
- Ce qui est simplifié sciemment porte un commentaire `ponytail:` qui nomme la limite et l'étape suivante.

## Signaler une faille

Pas d'issue publique : voir [SECURITY.md](SECURITY.md).
