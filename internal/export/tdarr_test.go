package export

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	transcodev1alpha1 "github.com/kyleseneker/media-operator/api/transcode/v1alpha1"
)

var fakeTdarrCollections = map[string]string{
	"LibrarySettingsJSONDB": `[{"_id":"movies","name":"Movies","folder":"/media/movies","handbrakescan":false,"ffmpegscan":true,
		"decisionMaker":{"settingsFlows":true,"settingsPlugin":false},"priority":1,"flowId":"flow1",
		"schedule":[{"checked":true},{"checked":false}],
		"pluginIDs":[{"_id":"plugin2","id":"Second","checked":true,"source":"Community","priority":1,"InputsDB":{}},
			{"_id":"plugin1","id":"First","checked":true,"source":"Community","priority":0,"InputsDB":{}},
			{"_id":"plugin3","id":"Off","checked":false,"source":"Community","priority":2,"InputsDB":{}}],"variables":{"user":{"fromDoc":"a","shared":"doc"}}},
		{"_id":"tv","name":"TV","folder":"/media/tv","decisionMaker":{"settingsFlows":false,"settingsPlugin":false,"settingsVideo":true}}]`,
	"VariablesJSONDB": `[{"key":"shared","value":"db","type":"library:movies"},{"key":"other","value":"x","type":"library:tv"},
		{"key":"global","value":"g","type":"global"}]`,
	"FlowsJSONDB": `[{"_id":"flow1","name":"Remux","description":"","flowPlugins":[{"id":"n1"}],"flowEdges":[]}]`,
}

func TestTdarrExportsLibrariesFlowsAndWorkers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/cruddb":
			var req struct {
				Data struct {
					Collection string `json:"collection"`
				} `json:"data"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			_, _ = w.Write([]byte(fakeTdarrCollections[req.Data.Collection]))
		case "/api/v2/get-nodes":
			_, _ = w.Write([]byte(`{"b":{"nodeName":"Zeta","workerLimits":{}},
				"a":{"nodeName":"InternalNode","workerLimits":{"transcodecpu":2,"healthcheckGPU":1}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	res, err := Run(context.Background(), Options{App: "tdarr", URL: srv.URL, APIKey: "k"})
	require.NoError(t, err)
	cfg, ok := res.Config.(*transcodev1alpha1.TdarrConfig)
	require.True(t, ok)

	require.Len(t, cfg.Spec.Libraries, 2)
	movies := cfg.Spec.Libraries[0]
	assert.Equal(t, "movies", movies.ID)
	assert.Equal(t, "thorough", movies.HealthCheckMode)
	assert.Equal(t, "flows", movies.DecisionMode)
	assert.Equal(t, map[string]string{"fromDoc": "a", "shared": "db"}, movies.Variables, "variable documents win over the library copy")
	assert.Contains(t, res.Warnings,
		`library "Movies": plugin Off is disabled, local or has options, which pluginStack can't express`)
	assert.Empty(t, cfg.Spec.Libraries[1].DecisionMode)
	assert.Contains(t, res.Warnings,
		`library "TV" decides by video and audio settings, which TdarrConfig can't express, so they stay as set in Tdarr`)

	require.Len(t, cfg.Spec.Flows, 1)
	assert.Equal(t, "flow1", cfg.Spec.Flows[0].ID)
	assert.JSONEq(t, `{"id":"n1"}`, string(cfg.Spec.Flows[0].FlowPlugins[0].Raw))

	assert.Equal(t, "InternalNode", cfg.Spec.Workers.NodeName)
	assert.Equal(t, 2, *cfg.Spec.Workers.TranscodeCPU)
	assert.Equal(t, 1, *cfg.Spec.Workers.HealthcheckGPU)
	assert.Nil(t, cfg.Spec.Workers.TranscodeGPU)
	assert.Contains(t, res.Warnings, `Tdarr has another node, "Zeta"; TdarrConfig sets workers on one node, so only "InternalNode" is exported`)
}
