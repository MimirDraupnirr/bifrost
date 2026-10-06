//go:build !noagents

package main

import "embed"

// Les binaires Linux de Bifröst lui-même, copiés sur la seedbox par SSH
// (docs/23 §4). Produits par `make agents` avant le build du poste ; un
// build de dev sans eux fonctionne, mais ne sait déployer l'agent que
// depuis un hôte Linux de même architecture (il se copie lui-même).
//
//go:embed agent
var agentFS embed.FS
