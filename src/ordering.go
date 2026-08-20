package main

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// scoreField 是 FTS 相關性分數的虛擬欄位名稱。它不是 SQL 表裡的欄位，
// 只有帶 search 子句的查詢才會產生它，所以用到它的排序必須在 Go 這側做。
const scoreField = "_score"

// ValidationError 表示請求本身有問題，例如 order_by 指到不存在的欄位。
// handleQuery 用它把錯誤回成 400，而不是 500。
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

func newValidationError(format string, args ...interface{}) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// validateOrderBy 檢查 order_by 的每個欄位都存在於 collection schema 中。
// 欄位名稱寫錯時回傳 ValidationError；靜默忽略會讓呼叫端看不出自己弄錯。
// 回傳值 usesScore 指出排序是否用到 _score。
func validateOrderBy(orderBy []OrderBySpec, schema *CollectionSchema) (bool, error) {
	known := make(map[string]struct{}, len(schema.Fields)+1)
	known[schema.PrimaryKey] = struct{}{}
	for _, field := range schema.Fields {
		known[field.Name] = struct{}{}
	}

	usesScore := false
	for _, order := range orderBy {
		switch strings.ToLower(strings.TrimSpace(order.Direction)) {
		case "", "asc", "desc":
		default:
			return false, newValidationError(
				"invalid order_by direction %q for field %q: expected \"asc\" or \"desc\"",
				order.Direction, order.Field)
		}

		if order.Field == scoreField {
			usesScore = true
			continue
		}

		if !isValidIdentifier(order.Field) {
			return false, newValidationError("invalid order_by field: %q", order.Field)
		}
		if _, ok := known[order.Field]; !ok {
			return false, newValidationError(
				"unknown order_by field %q in collection %q", order.Field, schema.Name)
		}
	}

	return usesScore, nil
}

// orderByUsesScore 回報 order_by 是否引用 _score。
func orderByUsesScore(orderBy []OrderBySpec) bool {
	for _, order := range orderBy {
		if order.Field == scoreField {
			return true
		}
	}
	return false
}

// buildOrderByClause 產生 SQL 的 ORDER BY 子句。呼叫端必須先用 validateOrderBy
// 把欄位名稱對照 schema 驗證過，這裡才可以直接把欄位名稱串進 SQL。
func buildOrderByClause(orderBy []OrderBySpec) string {
	if len(orderBy) == 0 {
		return ""
	}

	clauses := make([]string, 0, len(orderBy))
	for _, order := range orderBy {
		direction := "ASC"
		if isDescending(order.Direction) {
			direction = "DESC"
		}
		clauses = append(clauses, order.Field+" "+direction)
	}

	return " ORDER BY " + strings.Join(clauses, ", ")
}

// buildLimitClause 產生 SQL 的 LIMIT/OFFSET 子句。SQLite 的 OFFSET 必須跟在
// LIMIT 後面，所以只指定 offset 時用 LIMIT -1 表示不限制筆數。
func buildLimitClause(limit, offset int) string {
	if limit <= 0 && offset <= 0 {
		return ""
	}

	count := "-1"
	if limit > 0 {
		count = strconv.Itoa(limit)
	}

	clause := " LIMIT " + count
	if offset > 0 {
		clause += " OFFSET " + strconv.Itoa(offset)
	}
	return clause
}

// applyLimitOffset 在 Go 這側套用 limit 與 offset。
func applyLimitOffset(records []map[string]interface{}, limit, offset int) []map[string]interface{} {
	start := offset
	if start < 0 {
		start = 0
	}
	if start >= len(records) {
		return []map[string]interface{}{}
	}

	records = records[start:]
	if limit > 0 && limit < len(records) {
		records = records[:limit]
	}
	return records
}

// sortRecords 依 order_by 對記錄排序。order_by 為空且結果帶 FTS 分數時，
// 退回預設的相關性排序（最相關在前）。
func sortRecords(records []map[string]interface{}, orderBy []OrderBySpec, hasScores bool) {
	specs := orderBy
	if len(specs) == 0 {
		if !hasScores {
			return
		}
		specs = []OrderBySpec{{Field: scoreField, Direction: "desc"}}
	}

	sort.SliceStable(records, func(i, j int) bool {
		for _, spec := range specs {
			cmp := compareValues(
				recordSortValue(records[i], spec.Field),
				recordSortValue(records[j], spec.Field),
			)
			if cmp == 0 {
				continue
			}
			if ascendingByValue(spec) {
				return cmp < 0
			}
			return cmp > 0
		}
		return false
	})
}

// recordSortValue 取出記錄中用來排序的值。沒有分數的記錄視為相關性最低，
// 所以 _score 缺席時用 +Inf 代替，讓它排在有分數的記錄之後。
func recordSortValue(record map[string]interface{}, field string) interface{} {
	value, ok := record[field]
	if !ok && field == scoreField {
		return math.Inf(1)
	}
	return value
}

// ascendingByValue 回報這個排序條件在「數值大小」上是不是升冪。
//
// _score 是相關性排名，不是一般數值欄位：ftscore 回傳的分數越小代表越相關，
// 所以 direction="desc"（相關性由高到低）對應到數值的升冪，反之亦然。
func ascendingByValue(spec OrderBySpec) bool {
	descending := isDescending(spec.Direction)
	if spec.Field == scoreField {
		return descending
	}
	return !descending
}

func isDescending(direction string) bool {
	return strings.EqualFold(strings.TrimSpace(direction), "desc")
}

// SQLite 的跨型別排序順序：NULL < 數值 < 文字 < BLOB。
const (
	rankNull = iota
	rankNumber
	rankText
	rankBlob
)

// compareValues 依 SQLite 的型別排序規則比較兩個欄位值，
// 回傳 -1、0 或 1。
func compareValues(a, b interface{}) int {
	rankA, rankB := valueRank(a), valueRank(b)
	if rankA != rankB {
		if rankA < rankB {
			return -1
		}
		return 1
	}

	switch rankA {
	case rankNull:
		return 0
	case rankNumber:
		return compareNumbers(a, b)
	case rankBlob:
		return bytes.Compare(a.([]byte), b.([]byte))
	default:
		return strings.Compare(toText(a), toText(b))
	}
}

func valueRank(value interface{}) int {
	switch value.(type) {
	case nil:
		return rankNull
	case bool, int, int32, int64, float32, float64:
		return rankNumber
	case []byte:
		return rankBlob
	default:
		return rankText
	}
}

// compareNumbers 比較兩個數值。兩邊都是整數時用 int64 比較，
// 避免大整數轉成 float64 之後失去精度。
func compareNumbers(a, b interface{}) int {
	intA, okA := toInt64(a)
	intB, okB := toInt64(b)
	if okA && okB {
		switch {
		case intA < intB:
			return -1
		case intA > intB:
			return 1
		default:
			return 0
		}
	}

	floatA, floatB := toFloat64(a), toFloat64(b)
	switch {
	case floatA < floatB:
		return -1
	case floatA > floatB:
		return 1
	default:
		return 0
	}
}

func toInt64(value interface{}) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

func toFloat64(value interface{}) float64 {
	switch v := value.(type) {
	case float32:
		return float64(v)
	case float64:
		return v
	default:
		if i, ok := toInt64(value); ok {
			return float64(i)
		}
		return 0
	}
}

func toText(value interface{}) string {
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", value)
}
