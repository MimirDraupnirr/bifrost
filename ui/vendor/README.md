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
