package pocketfts

import (
	"encoding/json"
	"fmt"
	"strings"
)

type QueryRequest struct {
	Collection string     `json:"collection"`
	Query      QueryNode  `json:"query"`
	Result     ResultSpec `json:"result,omitempty"`
}

type FlatQueryRequest struct {
	Collection string          `json:"collection"`
	Search     *FlatSearchSpec `json:"search,omitempty"`
	SQL        [][]interface{} `json:"sql,omitempty"`
	Limit      int             `json:"limit,omitempty"`
	Offset     int             `json:"offset,omitempty"`
	OrderBy    []OrderBySpec   `json:"order_by,omitempty"`
}

type FlatSearchSpec struct {
	Term string `json:"term"`
}

type QueryNode struct {
	// Logical operators
	And []*QueryNode `json:"$and,omitempty"`
	Or  []*QueryNode `json:"$or,omitempty"`
	Not *QueryNode   `json:"$not,omitempty"`

	// Query types
	SQL    *SQLQuery    `json:"sql,omitempty"`
	Search *SearchQuery `json:"search,omitempty"`
}

type SQLQuery struct {
	Where map[string]interface{} `json:"where"`
}

type SearchQuery struct {
	Term     string             `json:"term"`
	Fields   []string           `json:"fields,omitempty"`
	Weights  map[string]float64 `json:"weights,omitempty"`
	Operator string             `json:"operator,omitempty"` // "AND" or "OR"
}

type ResultSpec struct {
	Fields  []string      `json:"fields,omitempty"`
	Limit   int           `json:"limit,omitempty"`
	Offset  int           `json:"offset,omitempty"`
	OrderBy []OrderBySpec `json:"order_by,omitempty"`
}

type OrderBySpec struct {
	Field     string `json:"field"`
	Direction string `json:"direction"` // "asc" or "desc"
}

func (f *FlatQueryRequest) hasNewFormatFields() bool {
	if f == nil {
		return false
	}
	if f.Search != nil {
		return true
	}
	if f.SQL != nil {
		return true
	}
	if f.OrderBy != nil {
		return true
	}
	if f.Limit != 0 || f.Offset != 0 {
		return true
	}
	return false
}

func (f *FlatQueryRequest) toQueryRequest() (*QueryRequest, error) {
	if f == nil {
		return nil, fmt.Errorf("invalid request")
	}

	if strings.TrimSpace(f.Collection) == "" {
		return nil, fmt.Errorf("collection is required")
	}

	var nodes []*QueryNode
	hasSearch := false

	if f.Search != nil {
		term := strings.TrimSpace(f.Search.Term)
		if term != "" {
			nodes = append(nodes, &QueryNode{
				Search: &SearchQuery{Term: term},
			})
			hasSearch = true
		}
	}

	if len(f.SQL) > 0 {
		var clauses []map[string]interface{}

		for _, condition := range f.SQL {
			if len(condition) < 3 {
				return nil, fmt.Errorf("invalid sql condition: expected [field, operator, value]")
			}

			field, ok := condition[0].(string)
			if !ok || strings.TrimSpace(field) == "" {
				return nil, fmt.Errorf("invalid sql condition field")
			}

			operatorRaw, ok := condition[1].(string)
			if !ok || strings.TrimSpace(operatorRaw) == "" {
				return nil, fmt.Errorf("invalid sql condition operator")
			}

			mappedOp, err := mapSQLOperator(operatorRaw)
			if err != nil {
				return nil, err
			}

			var value interface{}
			if len(condition) > 2 {
				value = condition[2]
			}

			clause := map[string]interface{}{}
			if mappedOp == "$eq" {
				clause[field] = value
			} else {
				clause[field] = map[string]interface{}{mappedOp: value}
			}

			clauses = append(clauses, clause)
		}

		var where map[string]interface{}
		if len(clauses) == 1 {
			where = clauses[0]
		} else {
			andList := make([]interface{}, 0, len(clauses))
			for _, clause := range clauses {
				andList = append(andList, clause)
			}
			where = map[string]interface{}{"$and": andList}
		}

		nodes = append(nodes, &QueryNode{
			SQL: &SQLQuery{Where: where},
		})
	}

	var query QueryNode
	switch len(nodes) {
	case 0:
		query = QueryNode{SQL: &SQLQuery{Where: map[string]interface{}{}}}
	case 1:
		query = *nodes[0]
	default:
		query = QueryNode{And: nodes}
	}

	result := ResultSpec{
		Limit:  f.Limit,
		Offset: f.Offset,
	}
	if len(f.OrderBy) > 0 {
		result.OrderBy = f.OrderBy
	} else if hasSearch {
		result.OrderBy = []OrderBySpec{{Field: "_score", Direction: "desc"}}
	}

	return &QueryRequest{
		Collection: f.Collection,
		Query:      query,
		Result:     result,
	}, nil
}

func mapSQLOperator(op string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(op)) {
	case "=":
		return "$eq", nil
	case "!=":
		return "$ne", nil
	case ">":
		return "$gt", nil
	case ">=":
		return "$gte", nil
	case "<":
		return "$lt", nil
	case "<=":
		return "$lte", nil
	case "LIKE":
		return "$like", nil
	default:
		return "", fmt.Errorf("unsupported sql operator: %s", op)
	}
}

// Node is one node of a query tree: $and, $or, $not, sql or search.
type Node = QueryNode

// Result controls which fields come back and in what order and page.
type Result = ResultSpec

// ParseNode decodes the JSON of a nested-format query tree.
func ParseNode(data []byte) (Node, error) {
	var node Node
	if err := json.Unmarshal(data, &node); err != nil {
		return Node{}, newValidationError("invalid query node: %v", err)
	}
	return node, nil
}

// ParseQuery decodes a /query request body in either the flat or the nested
// format. The flat format wins when the body carries any of its top-level
// keys; otherwise the body is read as the nested format.
func ParseQuery(body []byte) (*QueryRequest, error) {
	var flatReq FlatQueryRequest
	if err := json.Unmarshal(body, &flatReq); err == nil && flatReq.hasNewFormatFields() {
		converted, err := flatReq.toQueryRequest()
		if err != nil {
			return nil, &ValidationError{Message: err.Error()}
		}
		return converted, nil
	}

	var req QueryRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, &ValidationError{Message: "Invalid JSON format"}
	}
	return &req, nil
}
