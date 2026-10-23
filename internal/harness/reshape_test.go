package harness

import (
	"encoding/json"
	"reflect"
	"testing"
)

const sampleList = `{
  "metaData": {},
  "resource": {
    "totalCount": 1,
    "businessMappings": [{
      "uuid": "abc123",
      "name": "Teams",
      "accountId": "ACC",
      "costTargets": [{
        "name": "Platform",
        "rules": [
          {"viewConditions": [{
            "type": "VIEW_ID_CONDITION",
            "viewField": {"fieldId": "awsUsageaccountid", "fieldName": "Account", "identifier": "AWS", "identifierName": "AWS"},
            "viewOperator": "IN",
            "values": ["111", "222"]
          }]},
          {"viewConditions": [{
            "type": "VIEW_ID_CONDITION",
            "viewField": {"fieldId": "gcpProjectId", "fieldName": "Project", "identifier": "GCP", "identifierName": "GCP"},
            "viewOperator": "IN",
            "values": ["proj-a"]
          }]}
        ]
      }],
      "sharedCosts": [{"name": "Shared DB", "rules": [], "strategy": "PROPORTIONAL", "splits": []}],
      "unallocatedCost": {"strategy": "DISPLAY_NAME", "label": "Unattributed"},
      "dataSources": ["AWS", "GCP"],
      "createdAt": 1700000000000,
      "lastUpdatedAt": 1700000000000,
      "createdBy": {"uuid": "u1", "name": "Jane", "email": "jane@example.com"}
    }]
  },
  "responseMessages": []
}`

func decode(t *testing.T) []BusinessMapping {
	t.Helper()
	var env RestResponse[BusinessMappingList]
	if err := json.Unmarshal([]byte(sampleList), &env); err != nil {
		t.Fatal(err)
	}
	return env.Resource.BusinessMappings
}

func TestSummarize(t *testing.T) {
	s := Summarize(decode(t)[0])
	if s.Name != "Teams" || s.CostBucketCount != 1 || s.SharedBucketCount != 1 || s.RuleCount != 2 || s.ConditionCount != 2 {
		t.Fatalf("unexpected summary: %+v", s)
	}
	if s.Unallocated != "DISPLAY_NAME (Unattributed)" || s.CreatedBy != "jane@example.com" {
		t.Fatalf("unexpected summary: %+v", s)
	}
}

func TestFlattenUnflattenRoundTrip(t *testing.T) {
	in := decode(t)
	rows := FlattenAll(in)
	if len(rows) != 3 { // 2 conditions + 1 empty shared bucket
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	if rows[0].Values != "111|222" || rows[0].RuleIndex != 0 || rows[1].RuleIndex != 1 {
		t.Fatalf("unexpected rows: %+v", rows)
	}

	out := Unflatten(rows)
	if len(out) != 1 {
		t.Fatalf("want 1 category, got %d", len(out))
	}
	got, want := out[0], in[0]
	if got.UUID != want.UUID || got.Name != want.Name {
		t.Fatalf("identity mismatch: %+v", got)
	}
	if !reflect.DeepEqual(got.CostTargets, want.CostTargets) {
		t.Fatalf("costTargets mismatch\n got: %+v\nwant: %+v", got.CostTargets, want.CostTargets)
	}
	if len(got.SharedCosts) != 1 || got.SharedCosts[0].Name != "Shared DB" || got.SharedCosts[0].Strategy != "PROPORTIONAL" {
		t.Fatalf("sharedCosts mismatch: %+v", got.SharedCosts)
	}
}

func TestBucketMap(t *testing.T) {
	m := ToBucketMap(decode(t))
	exprs := m["Teams"]["Platform"]
	if len(exprs) != 2 || exprs[0] != "rule[0]: AWS.awsUsageaccountid IN 111|222" {
		t.Fatalf("unexpected bucket map: %+v", m)
	}
	if _, ok := m["Teams"]["shared:Shared DB"]; !ok {
		t.Fatalf("missing shared bucket: %+v", m)
	}
}

func TestPrettyRawLossless(t *testing.T) {
	pretty, err := PrettyRaw([]byte(sampleList))
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	_ = json.Unmarshal([]byte(sampleList), &a)
	_ = json.Unmarshal(pretty, &b)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("PrettyRaw altered content")
	}
}
