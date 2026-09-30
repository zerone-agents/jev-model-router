package management_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

func TestModelListSortAcrossPages(t *testing.T) {
	store := &memoryStore{s: routing.Snapshot{Version: 1, Models: []routing.Model{
		{ID: "a-disabled"}, {ID: "z-enabled", Enabled: true},
		{ID: "b-disabled"}, {ID: "y-enabled", Enabled: true},
	}}}
	service := management.New(store, nil)
	for _, tc := range []struct {
		name, order string
		want        []string
	}{
		{"default", "", []string{"a-disabled", "b-disabled", "y-enabled", "z-enabled"}},
		{"enabled first", "enabled_first", []string{"y-enabled", "z-enabled", "a-disabled", "b-disabled"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			cursor := ""
			for page := 0; page < 5; page++ {
				input := map[string]any{"limit": 1, "cursor": cursor}
				if tc.order != "" {
					input["sort"] = tc.order
				}
				raw, _ := json.Marshal(input)
				result, err := service.Execute(context.Background(), "settings", management.Call{CapabilityID: "models.list", Input: raw})
				if err != nil {
					t.Fatal(err)
				}
				var data struct {
					Items []routing.Model `json:"items"`
					Next  string          `json:"next_cursor"`
				}
				if err := json.Unmarshal(result.Data, &data); err != nil {
					t.Fatal(err)
				}
				if len(data.Items) != 1 {
					t.Fatalf("expected one item: %s", result.Data)
				}
				got = append(got, data.Items[0].ID)
				cursor = data.Next
				if cursor == "" {
					break
				}
			}
			if cursor != "" || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v cursor %q, want %v", got, cursor, tc.want)
			}
		})
	}
}

func TestModelListOffset(t *testing.T) {
	service := management.New(&memoryStore{s: routing.Snapshot{Models: []routing.Model{{ID: "a"}, {ID: "z", Enabled: true}, {ID: "b"}}}}, nil)
	for _, tc := range []struct {
		input string
		want  string
		size  int
	}{
		{`{"sort":"enabled_first","offset":1,"limit":1}`, "a", 1},
		{`{"sort":"enabled_first","offset":99,"limit":1}`, "", 0},
	} {
		r, e := service.Execute(context.Background(), "settings", management.Call{CapabilityID: "models.list", Input: json.RawMessage(tc.input)})
		if e != nil {
			t.Fatal(e)
		}
		var data struct {
			Total int             `json:"total"`
			Items []routing.Model `json:"items"`
		}
		if e := json.Unmarshal(r.Data, &data); e != nil {
			t.Fatal(e)
		}
		if data.Total != 3 || len(data.Items) != tc.size {
			t.Fatalf("unexpected page: %s", r.Data)
		}
		if tc.size > 0 && data.Items[0].ID != tc.want {
			t.Fatalf("unexpected model: %s", r.Data)
		}
	}
	_, err := service.Execute(context.Background(), "settings", management.Call{CapabilityID: "models.list", Input: json.RawMessage(`{"offset":0,"cursor":"a"}`)})
	if err == nil {
		t.Fatal("accepted mixed pagination modes")
	}
}

func TestModelSearchBeforePagination(t *testing.T) {
	service := management.New(&memoryStore{s: routing.Snapshot{Models: []routing.Model{
		{ID: "first", Description: "ordinary"},
		{ID: "second", ProviderID: "vendor", UpstreamName: "upstream", Description: "Long tail NEEDLE", Enabled: true, Capabilities: routing.Capabilities{Tools: true}},
	}}}, nil)
	for _, query := range []string{"needle", "vendor / upstream", "Tools", "Enabled", "second"} {
		input, _ := json.Marshal(map[string]any{"query": query, "limit": 1})
		r, e := service.Execute(context.Background(), "settings", management.Call{CapabilityID: "models.list", Input: input})
		if e != nil {
			t.Fatal(e)
		}
		var data struct {
			Total int             `json:"total"`
			Items []routing.Model `json:"items"`
		}
		json.Unmarshal(r.Data, &data)
		if data.Total != 1 || len(data.Items) != 1 || data.Items[0].ID != "second" {
			t.Fatalf("query %q: %s", query, r.Data)
		}
	}
}
