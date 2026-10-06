//go:build noagents

package main

import "embed"

// Build de l'agent lui-même : rien d'embarqué, sinon chaque agent
// contiendrait les agents précédents.
var agentFS embed.FS
