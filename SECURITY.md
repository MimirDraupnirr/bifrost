# Sécurité

## Signaler une vulnérabilité

Utilise **Security › Report a vulnerability** sur le dépôt GitHub (signalement privé). N'ouvre pas d'issue publique pour une faille, et n'y colle jamais de jeton, de clé ou de configuration.

Tu recevras un accusé de réception sous 7 jours. Les failles confirmées sont corrigées dans une release signée, et créditées si tu le souhaites.

## Périmètre

Sont concernés : le binaire Bifröst, sa page locale, l'agent déployé sur une seedbox, le mécanisme de mise à jour et le workflow de release. Le site Draupnirr lui-même a son propre canal.

## Ce que Bifröst garantit

- **Aucun identifiant ne transite par Draupnirr** : clé et mot de passe SSH, identifiants du client torrent, chemins et contenu du client restent sur ta machine. Seul le jeton API, émis par le site, lui est présenté.
- Le mot de passe SSH n'est jamais écrit sur disque. La configuration est écrite en mode `0600`.
- Hors loopback, la page exige un mot de passe local (argon2id, cookie de session, pause croissante sur échec). La page refuse les requêtes dont l'origine n'est pas la sienne.
- Les mises à jour sont vérifiées : signature ed25519 de `checksums.txt` avec la clé publique embarquée dans le binaire, puis SHA-256 de l'asset. Rien n'est installé si l'une des deux ne colle pas.
- La clé privée de signature n'existe que dans un secret du dépôt ; une rotation est annoncée par une release signée avec l'ancienne clé.

## Versions prises en charge

Seule la dernière release reçoit des correctifs. Le binaire se met à jour tout seul au lancement, sauf si tu as désactivé l'option.
