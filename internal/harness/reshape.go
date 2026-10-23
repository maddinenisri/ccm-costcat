package harness

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---- Summary view ----------------------------------------------------------

// CategorySummary is a compact, human-oriented view of a cost category.
type CategorySummary struct {
	UUID              string   `json:"uuid"`
	Name              string   `json:"name"`
	DataSources       []string `json:"dataSources,omitempty"`
	CostBucketCount   int      `json:"costBucketCount"`
	SharedBucketCount int      `json:"sharedBucketCount"`
	RuleCount         int      `json:"ruleCount"`
	ConditionCount    int      `json:"conditionCount"`
	CostBuckets       []string `json:"costBuckets,omitempty"`
	SharedBuckets     []string `json:"sharedBuckets,omitempty"`
	Unallocated       string   `json:"unallocated,omitempty"`
	OverlapState      string   `json:"overlapValidationState,omitempty"`
	CreatedAt         string   `json:"createdAt,omitempty"`
	LastUpdatedAt     string   `json:"lastUpdatedAt,omitempty"`
	CreatedBy         string   `json:"createdBy,omitempty"`
	LastUpdatedBy     string   `json:"lastUpdatedBy,omitempty"`
}

func Summarize(bm BusinessMapping) CategorySummary {
	s := CategorySummary{
		UUID:          bm.UUID,
		Name:          bm.Name,
		DataSources:   bm.DataSources,
		OverlapState:  bm.OverlapValidationState,
		CreatedAt:     msToRFC3339(bm.CreatedAt),
		LastUpdatedAt: msToRFC3339(bm.LastUpdatedAt),
		CreatedBy:     userLabel(bm.CreatedBy),
		LastUpdatedBy: userLabel(bm.LastUpdatedBy),
	}
	for _, ct := range bm.CostTargets {
		s.CostBuckets = append(s.CostBuckets, ct.Name)
		s.RuleCount += len(ct.Rules)
		for _, r := range ct.Rules {
			s.ConditionCount += len(r.ViewConditions)
		}
	}
	for _, sc := range bm.SharedCosts {
		s.SharedBuckets = append(s.SharedBuckets, sc.Name)
		s.RuleCount += len(sc.Rules)
		for _, r := range sc.Rules {
			s.ConditionCount += len(r.ViewConditions)
		}
	}
	s.CostBucketCount = len(bm.CostTargets)
	s.SharedBucketCount = len(bm.SharedCosts)
	if bm.UnallocatedCost != nil {
		s.Unallocated = bm.UnallocatedCost.Strategy
		if bm.UnallocatedCost.Label != "" {
			s.Unallocated += " (" + bm.UnallocatedCost.Label + ")"
		}
	}
	return s
}

func SummarizeAll(bms []BusinessMapping) []CategorySummary {
	out := make([]CategorySummary, 0, len(bms))
	for _, bm := range bms {
		out = append(out, Summarize(bm))
	}
	return out
}

// ---- Flat rows -------------------------------------------------------------

// FlatRow is one condition of one rule of one bucket. ruleIndex/conditionIndex
// preserve the OR/AND structure so rows can be reshaped back into a category.
type FlatRow struct {
	CategoryUUID   string `json:"categoryUuid"`
	CategoryName   string `json:"categoryName"`
	BucketType     string `json:"bucketType"` // cost | shared
	BucketName     string `json:"bucketName"`
	BucketStrategy string `json:"bucketStrategy,omitempty"` // shared buckets only
	RuleIndex      int    `json:"ruleIndex"`
	ConditionIndex int    `json:"conditionIndex"`
	ConditionType  string `json:"conditionType,omitempty"`
	Identifier     string `json:"identifier"`
	IdentifierName string `json:"identifierName,omitempty"`
	FieldID        string `json:"fieldId"`
	FieldName      string `json:"fieldName,omitempty"`
	Operator       string `json:"operator"`
	Values         string `json:"values"` // "|"-joined
}

const valueSep = "|"

func Flatten(bm BusinessMapping) []FlatRow {
	var rows []FlatRow
	emit := func(btype, bname, strategy string, rules []ViewRule) {
		if len(rules) == 0 {
			rows = append(rows, FlatRow{
				CategoryUUID: bm.UUID, CategoryName: bm.Name,
				BucketType: btype, BucketName: bname, BucketStrategy: strategy,
				RuleIndex: -1, ConditionIndex: -1,
			})
			return
		}
		for ri, r := range rules {
			if len(r.ViewConditions) == 0 {
				rows = append(rows, FlatRow{
					CategoryUUID: bm.UUID, CategoryName: bm.Name,
					BucketType: btype, BucketName: bname, BucketStrategy: strategy,
					RuleIndex: ri, ConditionIndex: -1,
				})
				continue
			}
			for ci, c := range r.ViewConditions {
				rows = append(rows, FlatRow{
					CategoryUUID:   bm.UUID,
					CategoryName:   bm.Name,
					BucketType:     btype,
					BucketName:     bname,
					BucketStrategy: strategy,
					RuleIndex:      ri,
					ConditionIndex: ci,
					ConditionType:  c.Type,
					Identifier:     c.ViewField.Identifier,
					IdentifierName: c.ViewField.IdentifierName,
					FieldID:        c.ViewField.FieldID,
					FieldName:      c.ViewField.FieldName,
					Operator:       c.ViewOperator,
					Values:         strings.Join(c.Values, valueSep),
				})
			}
		}
	}
	for _, ct := range bm.CostTargets {
		emit("cost", ct.Name, "", ct.Rules)
	}
	for _, sc := range bm.SharedCosts {
		emit("shared", sc.Name, sc.Strategy, sc.Rules)
	}
	return rows
}

func FlattenAll(bms []BusinessMapping) []FlatRow {
	var rows []FlatRow
	for _, bm := range bms {
		rows = append(rows, Flatten(bm)...)
	}
	return rows
}

// Unflatten rebuilds categories from FlatRows (rules/conditions only; metadata
// such as unallocated cost, splits and timestamps is not carried by rows).
func Unflatten(rows []FlatRow) []BusinessMapping {
	type bucketKey struct{ cat, btype, name string }
	catOrder := []string{}
	cats := map[string]*BusinessMapping{}
	bucketOrder := map[string][]bucketKey{}
	rulesByBucket := map[bucketKey]map[int][]ViewCondition{}
	strategyByBucket := map[bucketKey]string{}

	for _, r := range rows {
		ck := r.CategoryUUID + "\x00" + r.CategoryName
		if _, ok := cats[ck]; !ok {
			cats[ck] = &BusinessMapping{UUID: r.CategoryUUID, Name: r.CategoryName}
			catOrder = append(catOrder, ck)
		}
		bk := bucketKey{ck, r.BucketType, r.BucketName}
		if _, ok := rulesByBucket[bk]; !ok {
			rulesByBucket[bk] = map[int][]ViewCondition{}
			bucketOrder[ck] = append(bucketOrder[ck], bk)
			strategyByBucket[bk] = r.BucketStrategy
		}
		if r.RuleIndex < 0 {
			continue
		}
		if r.ConditionIndex < 0 {
			if _, ok := rulesByBucket[bk][r.RuleIndex]; !ok {
				rulesByBucket[bk][r.RuleIndex] = nil
			}
			continue
		}
		var vals []string
		if r.Values != "" {
			vals = strings.Split(r.Values, valueSep)
		}
		rulesByBucket[bk][r.RuleIndex] = append(rulesByBucket[bk][r.RuleIndex], ViewCondition{
			Type:         r.ConditionType,
			ViewField:    ViewField{FieldID: r.FieldID, FieldName: r.FieldName, Identifier: r.Identifier, IdentifierName: r.IdentifierName},
			ViewOperator: r.Operator,
			Values:       vals,
		})
	}

	out := make([]BusinessMapping, 0, len(catOrder))
	for _, ck := range catOrder {
		bm := cats[ck]
		for _, bk := range bucketOrder[ck] {
			idx := make([]int, 0, len(rulesByBucket[bk]))
			for i := range rulesByBucket[bk] {
				idx = append(idx, i)
			}
			sort.Ints(idx)
			rules := make([]ViewRule, 0, len(idx))
			for _, i := range idx {
				conds := rulesByBucket[bk][i]
				if conds == nil {
					conds = []ViewCondition{}
				}
				rules = append(rules, ViewRule{ViewConditions: conds})
			}
			switch bk.btype {
			case "shared":
				bm.SharedCosts = append(bm.SharedCosts, SharedCost{Name: bk.name, Rules: rules, Strategy: strategyByBucket[bk]})
			default:
				bm.CostTargets = append(bm.CostTargets, CostTarget{Name: bk.name, Rules: rules})
			}
		}
		out = append(out, *bm)
	}
	return out
}

// ---- Bucket → key/value map view ------------------------------------------

// BucketMap is categoryName -> bucketName -> list of "identifier.fieldId OP v1|v2".
type BucketMap map[string]map[string][]string

func ToBucketMap(bms []BusinessMapping) BucketMap {
	m := BucketMap{}
	for _, bm := range bms {
		b := map[string][]string{}
		add := func(prefix, name string, rules []ViewRule) {
			key := name
			if prefix != "" {
				key = prefix + ":" + name
			}
			var exprs []string
			for ri, r := range rules {
				var parts []string
				for _, c := range r.ViewConditions {
					parts = append(parts, fmt.Sprintf("%s.%s %s %s",
						c.ViewField.Identifier, c.ViewField.FieldID, c.ViewOperator, strings.Join(c.Values, valueSep)))
				}
				exprs = append(exprs, fmt.Sprintf("rule[%d]: %s", ri, strings.Join(parts, " AND ")))
			}
			b[key] = exprs
		}
		for _, ct := range bm.CostTargets {
			add("", ct.Name, ct.Rules)
		}
		for _, sc := range bm.SharedCosts {
			add("shared", sc.Name, sc.Rules)
		}
		m[bm.Name] = b
	}
	return m
}

// ---- Output helpers --------------------------------------------------------

func PrettyJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// PrettyRaw re-indents raw JSON bytes without decoding into structs (lossless).
func PrettyRaw(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

func WriteCSV(w io.Writer, rows []FlatRow) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{
		"categoryUuid", "categoryName", "bucketType", "bucketName", "bucketStrategy",
		"ruleIndex", "conditionIndex", "conditionType", "identifier", "identifierName",
		"fieldId", "fieldName", "operator", "values",
	}); err != nil {
		return err
	}
	for _, r := range rows {
		if err := cw.Write([]string{
			r.CategoryUUID, r.CategoryName, r.BucketType, r.BucketName, r.BucketStrategy,
			strconv.Itoa(r.RuleIndex), strconv.Itoa(r.ConditionIndex), r.ConditionType,
			r.Identifier, r.IdentifierName, r.FieldID, r.FieldName, r.Operator, r.Values,
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func msToRFC3339(ms int64) string {
	if ms == 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

func userLabel(u *EmbeddedUser) string {
	if u == nil {
		return ""
	}
	switch {
	case u.Email != "":
		return u.Email
	case u.Name != "":
		return u.Name
	default:
		return u.UUID
	}
}
