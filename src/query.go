package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// QueryExecutor 處理複雜查詢的執行
type QueryExecutor struct {
	db  *sql.DB
	fts *FTS
}

// NewQueryExecutor 創建查詢執行器
func NewQueryExecutor(database *sql.DB, ftsEngine *FTS) *QueryExecutor {
	return &QueryExecutor{
		db:  database,
		fts: ftsEngine,
	}
}

// ExecuteQuery 執行查詢並返回結果，支持 FTS 分數合併
func (qe *QueryExecutor) ExecuteQuery(req *QueryRequest) ([]map[string]interface{}, error) {
	if !isValidIdentifier(req.Collection) {
		return nil, newValidationError("invalid collection name: %s", req.Collection)
	}

	// 先取得 schema。order_by 的欄位要對照 schema 驗證，而且必須在執行查詢之前
	// 就驗證完，這樣欄位名稱寫錯的請求即使查不到任何資料也會回報錯誤。
	schema, err := qe.getCollectionSchema(req.Collection)
	if err != nil {
		return nil, fmt.Errorf("failed to get collection schema: %w", err)
	}

	usesScore, err := validateOrderBy(req.Result.OrderBy, schema)
	if err != nil {
		return nil, err
	}
	if usesScore && !queryHasSearch(&req.Query) {
		return nil, newValidationError(
			"order_by references %q but the query has no search clause", scoreField)
	}

	if records, handled, err := qe.executeRelevanceTopN(req, schema); handled {
		return records, err
	}

	// 把整棵查詢樹編譯成一條 SQL 的 WHERE 子句。search 節點在編譯過程中就向
	// ftscore 取得命中的主鍵，並以單一 JSON 參數帶進 SQL。
	compiler := newSQLCompiler(qe, req.Collection, schema.PrimaryKey)
	where, args, err := compiler.compile(&req.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	records, err := qe.fetchRecords(req.Collection, schema.PrimaryKey, where, args, compiler.scores, &req.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch records: %w", err)
	}

	return records, nil
}

// executeRelevanceTopN 處理「單一 search 節點、依相關性排序、有 limit」這個
// 最常見的搜尋情境。這種查詢只需要分數最小的前 offset+limit 筆，所以先用
// 堆積挑出來，再只取回那幾列，不必把全部命中的列都讀進來。
//
// handled 為 false 代表這個查詢不適用，呼叫端要走通用路徑。
func (qe *QueryExecutor) executeRelevanceTopN(req *QueryRequest, schema *CollectionSchema) ([]map[string]interface{}, bool, error) {
	if req.Query.Search == nil || req.Result.Limit <= 0 {
		return nil, false, nil
	}
	if !isRelevanceDescOrder(req.Result.OrderBy) {
		return nil, false, nil
	}

	offset := req.Result.Offset
	if offset < 0 {
		offset = 0
	}

	// 只需要最相關的前 offset+limit 筆，向 ftscore 就只要這麼多。
	results, err := qe.executeSearchQuery(req.Query.Search, req.Collection, offset+req.Result.Limit)
	if err != nil {
		return nil, true, fmt.Errorf("failed to execute query: %w", err)
	}
	if len(results) == 0 {
		return []map[string]interface{}{}, true, nil
	}

	ranked := topKRelevant(results, offset+req.Result.Limit)
	if offset >= len(ranked) {
		return []map[string]interface{}{}, true, nil
	}
	ranked = ranked[offset:]
	if len(ranked) > req.Result.Limit {
		ranked = ranked[:req.Result.Limit]
	}

	ids := make([]string, len(ranked))
	scores := make(ScoreMap, len(ranked))
	for i, result := range ranked {
		ids[i] = result.PrimaryKey
		scores[result.PrimaryKey] = result.Score
	}

	where, args, err := buildIDClause(schema.PrimaryKey, ids)
	if err != nil {
		return nil, true, err
	}

	// 分頁已經在挑選階段做完，取列時不再套用 limit 與 offset。
	spec := ResultSpec{Fields: req.Result.Fields}
	records, err := qe.fetchRecords(req.Collection, schema.PrimaryKey, where, args, scores, &spec)
	if err != nil {
		return nil, true, fmt.Errorf("failed to fetch records: %w", err)
	}

	sortRecords(records, []OrderBySpec{{Field: scoreField, Direction: "desc"}}, true)
	return records, true, nil
}

// isRelevanceDescOrder 回報這個 order_by 是不是「最相關在前」。
// 未指定 order_by 時，帶 search 的查詢預設就是這個順序。
func isRelevanceDescOrder(orderBy []OrderBySpec) bool {
	if len(orderBy) == 0 {
		return true
	}
	if len(orderBy) != 1 || orderBy[0].Field != scoreField {
		return false
	}
	// 只有明確寫 desc 才是「最相關在前」。direction 省略時視同 asc，
	// 對 _score 而言是「最不相關在前」，那要走通用路徑。
	return isDescending(orderBy[0].Direction)
}

// queryHasSearch 回報查詢樹裡是否含有 search 節點。只有這種查詢才會產生
// _score，所以 order_by 用到 _score 時要靠它判斷請求是否合法。
func queryHasSearch(node *QueryNode) bool {
	if node == nil {
		return false
	}
	if node.Search != nil {
		return true
	}
	for _, child := range node.And {
		if queryHasSearch(child) {
			return true
		}
	}
	for _, child := range node.Or {
		if queryHasSearch(child) {
			return true
		}
	}
	return queryHasSearch(node.Not)
}

// FTSResult 結構體包含主鍵和分數
type FTSResult struct {
	PrimaryKey string
	Score      float64
}

// QueryResult 統一的查詢結果結構，包含主鍵和可選的分數
type QueryResult struct {
	PrimaryKey string
	Score      *float64 // nil 表示不是 FTS 查詢結果
}

// ScoreMap 用於儲存 FTS 分數的映射
type ScoreMap map[string]float64

// allHits 是向 ftscore 索取「全部命中」時使用的 limit。
//
// ftscore 的 limit 預設是 20（repository.go），而且是它自己套用的，不是
// pocket-fts 傳的。呼叫端不指定 limit 時，一次搜尋就只會拿到 20 筆候選，
// 後續的 SQL 篩選與 order_by 也只作用在那 20 筆上，結果會安靜地出錯。
// 所以每次呼叫都必須明確指定要幾筆。
const allHits = math.MaxInt32

// executeSearchQuery 執行全文搜索查詢，返回包含分數的結果。
//
// limit 指定要向 ftscore 索取幾筆命中。ftscore 以 BM25 分數排序後才套用
// limit，所以取前 N 筆等於取最相關的 N 筆。要全部命中時傳 allHits。
func (qe *QueryExecutor) executeSearchQuery(searchQuery *SearchQuery, collection string, limit int) ([]FTSResult, error) {
	if limit <= 0 {
		limit = allHits
	}

	// 構建FTS搜索請求，不設置 ReturnFields 讓它只返回 PK 和 _score
	ftsRequest := map[string]interface{}{
		"query": searchQuery.Term,
		"limit": limit,
	}

	if len(searchQuery.Fields) > 0 {
		ftsRequest["fields"] = searchQuery.Fields
	}

	if len(searchQuery.Weights) > 0 {
		ftsRequest["weights"] = searchQuery.Weights
	}

	if searchQuery.Operator != "" {
		ftsRequest["operator"] = searchQuery.Operator
	}

	// 不設置 ReturnFields，確保只返回主鍵和分數

	// 序列化為JSON
	requestJSON, err := json.Marshal(ftsRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal FTS request: %w", err)
	}

	logger.Debug("Executing FTS search", "collection", collection, "request", string(requestJSON))

	// 執行FTS搜索
	resultJSON, err := qe.fts.Search(collection, string(requestJSON))
	if err != nil {
		return nil, fmt.Errorf("FTS search failed: %w", err)
	}

	// 解析FTS結果，包含分數
	var ftsResult struct {
		Hits []struct {
			ID    string  `json:"ID"`
			Score float64 `json:"Score"`
		} `json:"Hits"`
	}

	if err := json.Unmarshal([]byte(resultJSON), &ftsResult); err != nil {
		return nil, fmt.Errorf("failed to parse FTS result: %w", err)
	}

	// 提取主鍵和分數
	var results []FTSResult
	for _, hit := range ftsResult.Hits {
		results = append(results, FTSResult{
			PrimaryKey: hit.ID,
			Score:      hit.Score,
		})
	}

	return results, nil
}

// buildWhereClause 構建SQL WHERE子句
func (qe *QueryExecutor) buildWhereClause(conditions map[string]interface{}) (string, []interface{}, error) {
	if len(conditions) == 0 {
		return "", nil, nil
	}

	var clauses []string
	var args []interface{}

	for key, value := range conditions {
		switch key {
		case "$and":
			andClauses, andArgs, err := qe.buildAndCondition(value)
			if err != nil {
				return "", nil, err
			}
			if andClauses != "" {
				clauses = append(clauses, "("+andClauses+")")
				args = append(args, andArgs...)
			}

		case "$or":
			orClauses, orArgs, err := qe.buildOrCondition(value)
			if err != nil {
				return "", nil, err
			}
			if orClauses != "" {
				clauses = append(clauses, "("+orClauses+")")
				args = append(args, orArgs...)
			}

		case "$not":
			notClause, notArgs, err := qe.buildNotCondition(value)
			if err != nil {
				return "", nil, err
			}
			if notClause != "" {
				clauses = append(clauses, "NOT ("+notClause+")")
				args = append(args, notArgs...)
			}

		default:
			// 普通字段條件
			fieldClause, fieldArgs, err := qe.buildFieldCondition(key, value)
			if err != nil {
				return "", nil, err
			}
			if fieldClause != "" {
				clauses = append(clauses, fieldClause)
				args = append(args, fieldArgs...)
			}
		}
	}

	return strings.Join(clauses, " AND "), args, nil
}

// buildFieldCondition 構建字段條件
func (qe *QueryExecutor) buildFieldCondition(field string, value interface{}) (string, []interface{}, error) {
	if !isValidIdentifier(field) {
		return "", nil, fmt.Errorf("invalid field name: %s", field)
	}

	// 如果value是map，表示使用操作符
	if valueMap, ok := value.(map[string]interface{}); ok {
		return qe.buildOperatorCondition(field, valueMap)
	}

	// 簡單等值條件
	return field + " = ?", []interface{}{value}, nil
}

// buildOperatorCondition 構建操作符條件
func (qe *QueryExecutor) buildOperatorCondition(field string, operators map[string]interface{}) (string, []interface{}, error) {
	var clauses []string
	var args []interface{}

	for op, value := range operators {
		switch op {
		case "$eq":
			clauses = append(clauses, field+" = ?")
			args = append(args, value)

		case "$ne":
			clauses = append(clauses, field+" != ?")
			args = append(args, value)

		case "$gt":
			clauses = append(clauses, field+" > ?")
			args = append(args, value)

		case "$gte":
			clauses = append(clauses, field+" >= ?")
			args = append(args, value)

		case "$lt":
			clauses = append(clauses, field+" < ?")
			args = append(args, value)

		case "$lte":
			clauses = append(clauses, field+" <= ?")
			args = append(args, value)

		case "$in":
			if valueSlice, ok := value.([]interface{}); ok && len(valueSlice) > 0 {
				placeholders := make([]string, len(valueSlice))
				for i := range placeholders {
					placeholders[i] = "?"
				}
				clauses = append(clauses, field+" IN ("+strings.Join(placeholders, ", ")+")")
				args = append(args, valueSlice...)
			}

		case "$nin":
			if valueSlice, ok := value.([]interface{}); ok && len(valueSlice) > 0 {
				placeholders := make([]string, len(valueSlice))
				for i := range placeholders {
					placeholders[i] = "?"
				}
				clauses = append(clauses, field+" NOT IN ("+strings.Join(placeholders, ", ")+")")
				args = append(args, valueSlice...)
			}

		case "$like":
			clauses = append(clauses, field+" LIKE ?")
			args = append(args, value)

		case "$contains":
			clauses = append(clauses, field+" LIKE ?")
			args = append(args, "%"+fmt.Sprintf("%v", value)+"%")

		case "$null":
			if boolValue, ok := value.(bool); ok {
				if boolValue {
					clauses = append(clauses, field+" IS NULL")
				} else {
					clauses = append(clauses, field+" IS NOT NULL")
				}
			}

		case "$not_null":
			if boolValue, ok := value.(bool); ok {
				if boolValue {
					clauses = append(clauses, field+" IS NOT NULL")
				} else {
					clauses = append(clauses, field+" IS NULL")
				}
			}

		default:
			return "", nil, fmt.Errorf("unsupported operator: %s", op)
		}
	}

	return strings.Join(clauses, " AND "), args, nil
}

// buildAndCondition 構建AND條件
func (qe *QueryExecutor) buildAndCondition(value interface{}) (string, []interface{}, error) {
	andSlice, ok := value.([]interface{})
	if !ok {
		return "", nil, fmt.Errorf("$and must be an array")
	}

	var clauses []string
	var args []interface{}

	for _, item := range andSlice {
		if itemMap, ok := item.(map[string]interface{}); ok {
			clause, itemArgs, err := qe.buildWhereClause(itemMap)
			if err != nil {
				return "", nil, err
			}
			if clause != "" {
				clauses = append(clauses, "("+clause+")")
				args = append(args, itemArgs...)
			}
		}
	}

	return strings.Join(clauses, " AND "), args, nil
}

// buildOrCondition 構建OR條件
func (qe *QueryExecutor) buildOrCondition(value interface{}) (string, []interface{}, error) {
	orSlice, ok := value.([]interface{})
	if !ok {
		return "", nil, fmt.Errorf("$or must be an array")
	}

	var clauses []string
	var args []interface{}

	for _, item := range orSlice {
		if itemMap, ok := item.(map[string]interface{}); ok {
			clause, itemArgs, err := qe.buildWhereClause(itemMap)
			if err != nil {
				return "", nil, err
			}
			if clause != "" {
				clauses = append(clauses, "("+clause+")")
				args = append(args, itemArgs...)
			}
		}
	}

	return strings.Join(clauses, " OR "), args, nil
}

// buildNotCondition 構建NOT條件
func (qe *QueryExecutor) buildNotCondition(value interface{}) (string, []interface{}, error) {
	if valueMap, ok := value.(map[string]interface{}); ok {
		return qe.buildWhereClause(valueMap)
	}
	return "", nil, fmt.Errorf("$not must be an object")
}

// getCollectionSchema 獲取collection schema
func (qe *QueryExecutor) getCollectionSchema(collection string) (*CollectionSchema, error) {
	schemaJSON, err := getCollectionSchema(collection)
	if err != nil {
		return nil, err
	}

	var schema CollectionSchema
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		return nil, fmt.Errorf("failed to parse collection schema: %w", err)
	}

	return &schema, nil
}

// fetchRecords 依編譯好的 WHERE 子句取回整列資料，加入 FTS 分數，
// 並套用 order_by、limit 與 offset。
func (qe *QueryExecutor) fetchRecords(collection, primaryKeyField, where string, args []interface{}, scoreMap ScoreMap, result *ResultSpec) ([]map[string]interface{}, error) {
	// 構建查詢字段
	fields := "*"
	if len(result.Fields) > 0 {
		// 驗證字段名稱
		for _, field := range result.Fields {
			if !isValidIdentifier(field) && field != "*" {
				return nil, newValidationError("invalid field name: %s", field)
			}
		}
		fields = strings.Join(result.Fields, ", ")
	}

	query := fmt.Sprintf("SELECT %s FROM %s", fields, collection)
	if where != "" {
		query += " WHERE " + where
	}

	// _score 不是 SQL 表裡的欄位，所以只要排序用到它，整批記錄就得先讀回來
	// 再於 Go 這側排序。其餘情況把 ORDER BY 與 LIMIT/OFFSET 交給 SQLite，
	// 直接沿用 SQL 的比較與定序語意，也讓分頁在資料庫端就切好。
	sortInGo := orderByUsesScore(result.OrderBy) ||
		(len(result.OrderBy) == 0 && len(scoreMap) > 0)
	if !sortInGo {
		query += buildOrderByClause(result.OrderBy)
		query += buildLimitClause(result.Limit, result.Offset)
	}

	logger.Debug("Fetching records", "query", query, "arg_count", len(args))

	rows, err := qe.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch records: %w", err)
	}
	defer rows.Close()

	// 獲取列名
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	records := []map[string]interface{}{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePointers := make([]interface{}, len(columns))
		for i := range values {
			valuePointers[i] = &values[i]
		}

		if err := rows.Scan(valuePointers...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		record := make(map[string]interface{})
		for i, column := range columns {
			record[column] = values[i]
		}

		// 加入FTS分數（如果有的話）
		if pkValue, ok := record[primaryKeyField]; ok {
			pkStr := fmt.Sprintf("%v", pkValue)
			if score, hasScore := scoreMap[pkStr]; hasScore {
				record[scoreField] = score
			}
		}

		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read records: %w", err)
	}

	if sortInGo {
		sortRecords(records, result.OrderBy, len(scoreMap) > 0)
		records = applyLimitOffset(records, result.Limit, result.Offset)
	}

	return records, nil
}
