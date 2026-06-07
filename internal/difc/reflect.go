package difc

import (
	"sort"
	"time"

	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logReflect = logger.New("difc:reflect")

// ReflectedAgentLabels is the JSON shape for an agent's current DIFC labels.
type ReflectedAgentLabels struct {
	Secrecy   []string `json:"secrecy"`
	Integrity []string `json:"integrity"`
}

// ReflectResponse is the JSON response returned by /reflect endpoints.
type ReflectResponse struct {
	Agents    map[string]ReflectedAgentLabels `json:"agents"`
	Mode      string                          `json:"mode"`
	Timestamp string                          `json:"timestamp"`
}

// BuildReflectResponse returns a snapshot of all known agent labels.
func BuildReflectResponse(components DIFCComponents) ReflectResponse {
	logReflect.Printf("Building reflect response: mode=%s", components.Mode)
	agents := map[string]ReflectedAgentLabels{}
	if components.AgentRegistry != nil {
		agentIDs := components.AgentRegistry.GetAllAgentIDs()
		logReflect.Printf("Reflecting labels for %d registered agents", len(agentIDs))
		for _, agentID := range agentIDs {
			agent, ok := components.AgentRegistry.Get(agentID)
			if !ok || agent == nil {
				logReflect.Printf("Skipping agent %s: not found or nil in registry", agentID)
				continue
			}
			agents[agentID] = ReflectedAgentLabels{
				Secrecy:   tagsToStrings(agent.GetSecrecyTags()),
				Integrity: tagsToStrings(agent.GetIntegrityTags()),
			}
		}
	} else {
		logReflect.Print("No agent registry configured, returning empty agents map")
	}
	logReflect.Printf("Reflect response built: mode=%s, reflectedAgents=%d", components.Mode, len(agents))
	return ReflectResponse{
		Agents:    agents,
		Mode:      components.Mode.String(),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

func tagsToStrings(tags []Tag) []string {
	out := make([]string, len(tags))
	for i, tag := range tags {
		out[i] = string(tag)
	}
	sort.Strings(out)
	return out
}
