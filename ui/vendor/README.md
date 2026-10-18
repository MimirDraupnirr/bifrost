# Dépendances embarquées

Bifröst tourne en local et doit marcher sans réseau : rien n'est chargé
depuis un CDN.

## SweetAlert2 11.14.5 (MIT)

`sweetalert2.min.js` et `sweetalert2.min.css`, tirés du paquet npm
`sweetalert2@11.14.5` (`dist/`).

**Une modification** : la condition de la « protestware » de 11.x
(page bloquée et mp3 chargé depuis un site tiers pour une langue `ru` sur
un domaine `.ru/.su/.by/.рф`) est neutralisée (`!1&&location.host.match`).
Un outil local ne fait aucune requête qu'on ne lui a pas demandée. À refaire
à chaque montée de version :

    grep -c 'flag-gimn' sweetalert2.min.js    # le code mort reste, la condition doit commencer par !1&&

## IBM Plex Sans et Plex Mono (SIL OFL 1.1, `fonts/OFL.txt`)

`fonts/*.woff2`, tirés de `@fontsource/ibm-plex-sans@5.2.5` et
`@fontsource/ibm-plex-mono@5.2.5` (`files/`), sous-ensemble **latin** seul
(U+0000-00FF, Œ/œ, ponctuation générale U+2000-206F, €…) : de quoi écrire le
français. Un glyphe absent (→, par exemple) retombe sur la police système.
Sans 400/500/600/700, Mono 400/500.
