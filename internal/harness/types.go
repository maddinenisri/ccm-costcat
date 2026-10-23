package harness

import "encoding/json"

// RestResponse is the legacy Harness envelope used by the business-mapping endpoints.
type RestResponse[T any] struct {
	MetaData         map[string]any    `json:"metaData,omitempty"`
	Resource         T                 `json:"resource"`
	ResponseMessages []ResponseMessage `json:"responseMessages,omitempty"`
}

// ResponseDTO is the newer Harness envelope (used only by /filter-panel here).
type ResponseDTO[T any] struct {
	Status        string         `json:"status"`
	Data          T              `json:"data"`
	MetaData      map[string]any `json:"metaData,omitempty"`
	CorrelationID string         `json:"correlationId,omitempty"`
}

type ResponseMessage struct {
	Code           string            `json:"code,omitempty"`
	Level          string            `json:"level,omitempty"`
	Message        string            `json:"message,omitempty"`
	Exception      json.RawMessage   `json:"exception,omitempty"`
	FailureTypes   []string          `json:"failureTypes,omitempty"`
	AdditionalInfo map[string]string `json:"additionalInfo,omitempty"`
}

// APIError is the body returned on 4xx/5xx.
type APIError struct {
	Status          string            `json:"status,omitempty"`
	Code            string            `json:"code,omitempty"`
	Message         string            `json:"message,omitempty"`
	DetailedMessage string            `json:"detailedMessage,omitempty"`
	CorrelationID   string            `json:"correlationId,omitempty"`
	Errors          []FieldError      `json:"errors,omitempty"`
	ResponseMsgs    []ResponseMessage `json:"responseMessages,omitempty"`
}

type FieldError struct {
	FieldID string `json:"fieldId,omitempty"`
	Error   string `json:"error,omitempty"`
}

// BusinessMappingList is the resource of GET /business-mapping.
type BusinessMappingList struct {
	BusinessMappings []BusinessMapping `json:"businessMappings"`
	TotalCount       int               `json:"totalCount"`
}

// BusinessMapping is a Cost Category.
type BusinessMapping struct {
	UUID                     string                  `json:"uuid,omitempty"`
	Name                     string                  `json:"name"`
	AccountID                string                  `json:"accountId"`
	CostTargets              []CostTarget            `json:"costTargets,omitempty"`
	SharedCosts              []SharedCost            `json:"sharedCosts,omitempty"`
	UnallocatedCost          *UnallocatedCost        `json:"unallocatedCost,omitempty"`
	DataSources              []string                `json:"dataSources,omitempty"`
	OverlapValidationState   string                  `json:"overlapValidationState,omitempty"`
	CostBucketOverlapStats   []CostBucketOverlapStat `json:"costBucketOverlapStats,omitempty"`
	SupportsTagNormalization *bool                   `json:"supportsTagNormalization,omitempty"`
	CreatedAt                int64                   `json:"createdAt,omitempty"`
	LastUpdatedAt            int64                   `json:"lastUpdatedAt,omitempty"`
	CreatedBy                *EmbeddedUser           `json:"createdBy,omitempty"`
	LastUpdatedBy            *EmbeddedUser           `json:"lastUpdatedBy,omitempty"`
}

type CostTarget struct {
	Name  string     `json:"name"`
	Rules []ViewRule `json:"rules"`
}

type SharedCost struct {
	Name     string            `json:"name"`
	Rules    []ViewRule        `json:"rules"`
	Strategy string            `json:"strategy,omitempty"` // EQUAL | PROPORTIONAL | FIXED
	Splits   []SharedCostSplit `json:"splits,omitempty"`
}

type SharedCostSplit struct {
	CostTargetName         string  `json:"costTargetName"`
	PercentageContribution float64 `json:"percentageContribution"`
}

type UnallocatedCost struct {
	Strategy        string            `json:"strategy,omitempty"` // DISPLAY_NAME | HIDE | SHARE
	Label           string            `json:"label,omitempty"`
	SharingStrategy string            `json:"sharingStrategy,omitempty"` // EQUAL | PROPORTIONAL | FIXED
	Splits          []SharedCostSplit `json:"splits,omitempty"`
}

// ViewRule: conditions within a rule are AND-ed; rules within a bucket are OR-ed.
type ViewRule struct {
	ViewConditions []ViewCondition `json:"viewConditions"`
}

// ViewCondition is polymorphic on "type"; raw bytes are kept for lossless round-trip.
type ViewCondition struct {
	Type         string    `json:"type,omitempty"`
	ViewField    ViewField `json:"viewField"`
	ViewOperator string    `json:"viewOperator,omitempty"` // IN | NOT_IN | EQUALS | NULL | NOT_NULL | LIKE
	Values       []string  `json:"values,omitempty"`
}

type ViewField struct {
	FieldID        string `json:"fieldId,omitempty"`
	FieldName      string `json:"fieldName,omitempty"`
	Identifier     string `json:"identifier,omitempty"`
	IdentifierName string `json:"identifierName,omitempty"`
}

type CostBucketOverlapStat struct {
	CostBucketRef      CostBucketRef   `json:"costBucketRef"`
	Cost               float64         `json:"cost,omitempty"`
	Rows               int64           `json:"rows,omitempty"`
	OverlapCost        float64         `json:"overlapCost,omitempty"`
	OverlappingBuckets []CostBucketRef `json:"overlappingBuckets,omitempty"`
}

type CostBucketRef struct {
	UUID   string `json:"uuid,omitempty"`
	Name   string `json:"name,omitempty"`
	Shared bool   `json:"shared,omitempty"`
}

type EmbeddedUser struct {
	UUID           string `json:"uuid,omitempty"`
	Name           string `json:"name,omitempty"`
	Email          string `json:"email,omitempty"`
	ExternalUserID string `json:"externalUserId,omitempty"`
}

// CostCategoryDeleteResult is the resource of DELETE /business-mapping/{id}.
type CostCategoryDeleteResult struct {
	LinkedPerspectives    map[string]string `json:"linkedPerspectives,omitempty"`
	LinkedBusinessMapping map[string]string `json:"linkedBusinessMapping,omitempty"`
	Message               string            `json:"message,omitempty"`
	Deleted               bool              `json:"deleted"`
}

// LinkedBusinessMapping is the resource of POST /business-mapping/linkedBusinessMapping/{id}.
type LinkedBusinessMapping struct {
	BusinessMappingIDAndName map[string]string `json:"businessMappingIdAndName,omitempty"`
	BusinessMappingID        string            `json:"businessMappingId,omitempty"`
}

// LinkedPerspectives is one element of POST /business-mapping/linkedPerspectives.
// Shape is not fully documented in the OpenAPI; decoded loosely.
type LinkedPerspectives map[string]json.RawMessage

// ListOptions are the query params of GET /business-mapping.
type ListOptions struct {
	SearchKey string
	SortType  string // NAME | LAST_EDIT
	SortOrder string // ASCENDING | DESCENDING
	Limit     int
	Offset    int
}

// Sort / strategy constants.
const (
	SortTypeName     = "NAME"
	SortTypeLastEdit = "LAST_EDIT"
	SortAscending    = "ASCENDING"
	SortDescending   = "DESCENDING"

	StrategyEqual        = "EQUAL"
	StrategyProportional = "PROPORTIONAL"
	StrategyFixed        = "FIXED"

	UnallocatedDisplayName = "DISPLAY_NAME"
	UnallocatedHide        = "HIDE"
	UnallocatedShare       = "SHARE"

	OpIn      = "IN"
	OpNotIn   = "NOT_IN"
	OpEquals  = "EQUALS"
	OpNull    = "NULL"
	OpNotNull = "NOT_NULL"
	OpLike    = "LIKE"
)
