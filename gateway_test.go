package main_test

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"testing"
)

// TestGatewayRoutesMatchSpec keeps the Zuplo routes in step with openapi.yaml:
// every operation in the spec must be a gateway route and an MCP tool, and the
// gateway must not expose operations the API no longer has.
func TestGatewayRoutesMatchSpec(t *testing.T) {
	spec, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s+operationId:\s*(\S+)`).FindAllSubmatch(spec, -1) {
		want[string(m[1])] = true
	}
	if len(want) == 0 {
		t.Fatal("no operationIds in openapi.yaml")
	}

	raw, err := os.ReadFile("gateway/config/routes.oas.json")
	if err != nil {
		t.Fatal(err)
	}
	type op struct {
		OperationID string `json:"operationId"`
		Route       struct {
			Handler struct {
				Export  string `json:"export"`
				Options struct {
					Operations []struct {
						ID string `json:"id"`
					} `json:"operations"`
				} `json:"options"`
			} `json:"handler"`
		} `json:"x-zuplo-route"`
	}
	var gw struct {
		Paths map[string]map[string]op `json:"paths"`
	}
	if err := json.Unmarshal(raw, &gw); err != nil {
		t.Fatal(err)
	}
	routes, tools := map[string]bool{}, map[string]bool{}
	for _, methods := range gw.Paths {
		for _, o := range methods {
			if o.Route.Handler.Export == "mcpServerHandler" {
				for _, x := range o.Route.Handler.Options.Operations {
					tools[x.ID] = true
				}
				continue
			}
			routes[o.OperationID] = true
		}
	}
	diff(t, "gateway route", want, routes)
	diff(t, "MCP tool", want, tools)
}

func diff(t *testing.T, what string, want, got map[string]bool) {
	t.Helper()
	var missing, extra []string
	for k := range want {
		if !got[k] {
			missing = append(missing, k)
		}
	}
	for k := range got {
		if !want[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("operations in openapi.yaml with no %s: %v", what, missing)
	}
	if len(extra) > 0 {
		t.Errorf("%ss not in openapi.yaml: %v", what, extra)
	}
}
