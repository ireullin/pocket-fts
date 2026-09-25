package pocketfts

import (
	"fmt"
	"strconv"
	"strings"
)

// scoreField 是 FTS 相關性分數的虛擬欄位名稱。它不是 SQL 表裡的欄位，
// 只有帶 search 子句的查詢才會產生它；用到它的排序透過 pfts_score 函式交給
// SQLite（見 fetchRecords）。
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

// knownFieldNames 回傳這個 collection 的欄位名稱集合，含主鍵。
// SQLite 的欄位名稱不分大小寫，`SELECT ID` 取得的是宣告為 `id` 的那一欄，
// 所以鍵一律轉小寫，查表前也要轉。否則只是大小寫不同的請求會被擋掉。
func knownFieldNames(schema *CollectionSchema) map[string]struct{} {
	known := make(map[string]struct{}, len(schema.Fields)+1)
	known[strings.ToLower(schema.PrimaryKey)] = struct{}{}
	for _, field := range schema.Fields {
		known[strings.ToLower(field.Name)] = struct{}{}
	}
	return known
}

// validateOrderBy 檢查 order_by 的每個欄位都存在於 collection schema 中。
// 欄位名稱寫錯時回傳 ValidationError；靜默忽略會讓呼叫端看不出自己弄錯。
// 回傳值 usesScore 指出排序是否用到 _score。
func validateOrderBy(orderBy []OrderBySpec, schema *CollectionSchema) (bool, error) {
	known := knownFieldNames(schema)

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
		if _, ok := known[strings.ToLower(order.Field)]; !ok {
			return false, newValidationError(
				"unknown order_by field %q in collection %q", order.Field, schema.Name)
		}
	}

	return usesScore, nil
}

// validateResultFields 檢查 result.fields 的每個欄位都存在於 collection schema 中。
// 這份清單會被直接串進 SELECT，所以欄位名稱寫錯時必須在送出 SQL 之前回報。
// 交給 SQLite 抱怨的話，呼叫端拿到的是 HTTP 500 與一段 SQL 錯誤訊息，看不出
// 是自己把欄位名稱寫錯了。
//
// _score 的規則比照 order_by：只有帶 search 子句的查詢才產生分數。它不是 SQL
// 表裡的欄位，取回記錄時會依主鍵補上，所以選它就必須一起選主鍵。
func validateResultFields(fields []string, schema *CollectionSchema, hasSearch bool) error {
	known := knownFieldNames(schema)

	selectsScore := false
	selectsPrimaryKey := false
	for _, field := range fields {
		if field == "*" {
			selectsPrimaryKey = true
			continue
		}
		if field == scoreField {
			if !hasSearch {
				return newValidationError(
					"result.fields references %q but the query has no search clause", scoreField)
			}
			selectsScore = true
			continue
		}
		if !isValidIdentifier(field) {
			return newValidationError("invalid result field: %q", field)
		}
		if _, ok := known[strings.ToLower(field)]; !ok {
			return newValidationError("unknown result field %q in collection %q", field, schema.Name)
		}
		if strings.EqualFold(field, schema.PrimaryKey) {
			selectsPrimaryKey = true
		}
	}

	if selectsScore && !selectsPrimaryKey {
		return newValidationError(
			"result.fields references %q but does not select the primary key %q",
			scoreField, schema.PrimaryKey)
	}

	return nil
}

// selectableFields 從 result.fields 濾掉 _score，回傳真正能寫進 SELECT 的欄位。
// _score 是相關性排名，不是 SQL 表裡的欄位；取回記錄之後才依主鍵補上。
func selectableFields(fields []string) []string {
	selectable := make([]string, 0, len(fields))
	for _, field := range fields {
		if field == scoreField {
			continue
		}
		selectable = append(selectable, field)
	}
	return selectable
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

// buildScoreOrderByClause 產生 fetchRecords 依分數排序時的 ORDER BY 子句。
// order_by 為空時預設依相關性（最相關在前）。
//
// ftscore 的分數越小越相關，所以 _score desc（相關性由高到低）是分數的升冪。
// 沒有分數的列（$or 混合查詢才會出現）視為相關性最低：desc 時排在最後，
// asc 時排在最前。同分的列依主鍵升冪排列，最後以 rowid 保證順序確定；
// 這跟改用 SQL 排序之前，Go 穩定排序保留下來的取列順序一致。
func buildScoreOrderByClause(orderBy []OrderBySpec, primaryKey string) string {
	specs := orderBy
	if len(specs) == 0 {
		specs = []OrderBySpec{{Field: scoreField, Direction: "desc"}}
	}

	clauses := make([]string, 0, len(specs)+2)
	for _, order := range specs {
		if order.Field == scoreField {
			if isDescending(order.Direction) {
				clauses = append(clauses, "("+scoreColumn+" IS NULL) ASC", scoreColumn+" ASC")
			} else {
				clauses = append(clauses, "("+scoreColumn+" IS NULL) DESC", scoreColumn+" DESC")
			}
			continue
		}
		direction := "ASC"
		if isDescending(order.Direction) {
			direction = "DESC"
		}
		clauses = append(clauses, order.Field+" "+direction)
	}
	clauses = append(clauses, primaryKey+" ASC", "rowid ASC")

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

func isDescending(direction string) bool {
	return strings.EqualFold(strings.TrimSpace(direction), "desc")
}
