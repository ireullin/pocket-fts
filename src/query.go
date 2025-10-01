package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
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
		return nil, fmt.Errorf("invalid collection name: %s", req.Collection)
	}

	// 執行查詢獲取主鍵列表和分數映射
	primaryKeys, scoreMap, err := qe.executeQueryNodeWithScores(&req.Query, req.Collection)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	if len(primaryKeys) == 0 {
		return []map[string]interface{}{}, nil
	}

	// 獲取collection的schema以確定主鍵欄位名稱
	schema, err := qe.getCollectionSchema(req.Collection)
	if err != nil {
		return nil, fmt.Errorf("failed to get collection schema: %w", err)
	}

	// 根據主鍵列表從SQL表中獲取完整記錄，並加入分數
	records, err := qe.fetchRecordsByPrimaryKeysWithScores(req.Collection, schema.PrimaryKey, primaryKeys, scoreMap, &req.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch records: %w", err)
	}

	return records, nil
}

// executeQueryNodeWithScores 遞歸執行查詢節點，返回主鍵和分數映射
func (qe *QueryExecutor) executeQueryNodeWithScores(node *QueryNode, collection string) ([]string, ScoreMap, error) {
	// 處理邏輯操作符
	if node.And != nil {
		return qe.executeAndQueryWithScores(node.And, collection)
	}
	if node.Or != nil {
		return qe.executeOrQueryWithScores(node.Or, collection)
	}
	if node.Not != nil {
		return qe.executeNotQueryWithScores(node.Not, collection)
	}

	// 處理查詢類型
	if node.SQL != nil {
		primaryKeys, err := qe.executeSQLQuery(node.SQL, collection)
		return primaryKeys, make(ScoreMap), err // SQL 查詢不產生分數
	}
	if node.Search != nil {
		return qe.executeSearchQueryWithScores(node.Search, collection)
	}

	return nil, make(ScoreMap), fmt.Errorf("empty query node")
}

// executeQueryNode 遞歸執行查詢節點 (保留原有方法以維持向後兼容)
func (qe *QueryExecutor) executeQueryNode(node *QueryNode, collection string) ([]string, error) {
	primaryKeys, _, err := qe.executeQueryNodeWithScores(node, collection)
	return primaryKeys, err
}

// executeAndQuery 執行AND查詢
func (qe *QueryExecutor) executeAndQuery(nodes []*QueryNode, collection string) ([]string, error) {
	if len(nodes) == 0 {
		return []string{}, nil
	}

	// 執行第一個查詢
	result, err := qe.executeQueryNode(nodes[0], collection)
	if err != nil {
		return nil, err
	}

	// 與其他查詢結果取交集
	for i := 1; i < len(nodes); i++ {
		otherResult, err := qe.executeQueryNode(nodes[i], collection)
		if err != nil {
			return nil, err
		}
		result = intersectSlices(result, otherResult)
		
		// 短路：如果結果為空，可以提前返回
		if len(result) == 0 {
			return result, nil
		}
	}

	return result, nil
}

// executeOrQuery 執行OR查詢
func (qe *QueryExecutor) executeOrQuery(nodes []*QueryNode, collection string) ([]string, error) {
	if len(nodes) == 0 {
		return []string{}, nil
	}

	var allResults []string

	for _, node := range nodes {
		result, err := qe.executeQueryNode(node, collection)
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, result...)
	}

	// 去重並排序
	return uniqueAndSort(allResults), nil
}

// executeNotQuery 執行NOT查詢
func (qe *QueryExecutor) executeNotQuery(node *QueryNode, collection string) ([]string, error) {
	// 先獲取所有記錄的主鍵
	allKeys, err := qe.getAllPrimaryKeys(collection)
	if err != nil {
		return nil, err
	}

	// 執行被排除的查詢
	excludeKeys, err := qe.executeQueryNode(node, collection)
	if err != nil {
		return nil, err
	}

	// 返回差集
	return differenceSlices(allKeys, excludeKeys), nil
}

// executeSQLQuery 執行SQL查詢
func (qe *QueryExecutor) executeSQLQuery(sqlQuery *SQLQuery, collection string) ([]string, error) {
	// 獲取collection schema
	schema, err := qe.getCollectionSchema(collection)
	if err != nil {
		return nil, err
	}

	// 構建SQL WHERE子句
	whereClause, args, err := qe.buildWhereClause(sqlQuery.Where)
	if err != nil {
		return nil, fmt.Errorf("failed to build WHERE clause: %w", err)
	}

	// 構建完整的SQL查詢
	query := fmt.Sprintf("SELECT %s FROM %s", schema.PrimaryKey, collection)
	if whereClause != "" {
		query += " WHERE " + whereClause
	}

	logger.Debug("Executing SQL query", "query", query, "args", args)

	// 執行查詢
	rows, err := qe.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute SQL query: %w", err)
	}
	defer rows.Close()

	// 收集主鍵
	var primaryKeys []string
	for rows.Next() {
		var pk string
		if err := rows.Scan(&pk); err != nil {
			return nil, fmt.Errorf("failed to scan primary key: %w", err)
		}
		primaryKeys = append(primaryKeys, pk)
	}

	return primaryKeys, nil
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

// executeSearchQuery 執行全文搜索查詢，返回包含分數的結果
func (qe *QueryExecutor) executeSearchQuery(searchQuery *SearchQuery, collection string) ([]FTSResult, error) {
	// 構建FTS搜索請求，不設置 ReturnFields 讓它只返回 PK 和 _score
	ftsRequest := map[string]interface{}{
		"query": searchQuery.Term,
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

// executeSearchQueryWithScores 執行全文搜索查詢，返回主鍵列表和分數映射
func (qe *QueryExecutor) executeSearchQueryWithScores(searchQuery *SearchQuery, collection string) ([]string, ScoreMap, error) {
	ftsResults, err := qe.executeSearchQuery(searchQuery, collection)
	if err != nil {
		return nil, nil, err
	}

	primaryKeys := make([]string, len(ftsResults))
	scoreMap := make(ScoreMap)
	
	for i, result := range ftsResults {
		primaryKeys[i] = result.PrimaryKey
		scoreMap[result.PrimaryKey] = result.Score
	}

	return primaryKeys, scoreMap, nil
}

// executeAndQueryWithScores 執行AND查詢，保留分數信息
func (qe *QueryExecutor) executeAndQueryWithScores(nodes []*QueryNode, collection string) ([]string, ScoreMap, error) {
	if len(nodes) == 0 {
		return []string{}, make(ScoreMap), nil
	}

	// 執行第一個查詢
	result, resultScores, err := qe.executeQueryNodeWithScores(nodes[0], collection)
	if err != nil {
		return nil, nil, err
	}

	// 與其他查詢結果取交集
	for i := 1; i < len(nodes); i++ {
		otherResult, otherScores, err := qe.executeQueryNodeWithScores(nodes[i], collection)
		if err != nil {
			return nil, nil, err
		}
		result = intersectSlices(result, otherResult)
		// 合併分數 - 對於交集中的項目，保留所有可用的分數
		for key, score := range otherScores {
			if _, exists := resultScores[key]; !exists {
				resultScores[key] = score
			}
		}
		
		// 短路：如果結果為空，可以提前返回
		if len(result) == 0 {
			return result, make(ScoreMap), nil
		}
	}

	// 過濾分數映射，只保留最終結果中的項目
	filteredScores := make(ScoreMap)
	for _, pk := range result {
		if score, exists := resultScores[pk]; exists {
			filteredScores[pk] = score
		}
	}

	return result, filteredScores, nil
}

// executeOrQueryWithScores 執行OR查詢，合併分數信息
func (qe *QueryExecutor) executeOrQueryWithScores(nodes []*QueryNode, collection string) ([]string, ScoreMap, error) {
	if len(nodes) == 0 {
		return []string{}, make(ScoreMap), nil
	}

	var allResults []string
	allScores := make(ScoreMap)

	for _, node := range nodes {
		result, scores, err := qe.executeQueryNodeWithScores(node, collection)
		if err != nil {
			return nil, nil, err
		}
		allResults = append(allResults, result...)
		// 合併分數
		for key, score := range scores {
			allScores[key] = score
		}
	}

	// 去重並排序
	uniqueResults := uniqueAndSort(allResults)
	
	// 過濾分數映射，只保留最終結果中的項目
	filteredScores := make(ScoreMap)
	for _, pk := range uniqueResults {
		if score, exists := allScores[pk]; exists {
			filteredScores[pk] = score
		}
	}

	return uniqueResults, filteredScores, nil
}

// executeNotQueryWithScores 執行NOT查詢，不保留被排除項目的分數
func (qe *QueryExecutor) executeNotQueryWithScores(node *QueryNode, collection string) ([]string, ScoreMap, error) {
	// 先獲取所有記錄的主鍵
	allKeys, err := qe.getAllPrimaryKeys(collection)
	if err != nil {
		return nil, nil, err
	}

	// 執行被排除的查詢
	excludeKeys, _, err := qe.executeQueryNodeWithScores(node, collection)
	if err != nil {
		return nil, nil, err
	}

	// 返回差集（NOT查詢不產生分數）
	result := differenceSlices(allKeys, excludeKeys)
	return result, make(ScoreMap), nil
}

// Helper functions for set operations
func intersectSlices(a, b []string) []string {
	m := make(map[string]bool)
	for _, item := range a {
		m[item] = true
	}

	var result []string
	for _, item := range b {
		if m[item] {
			result = append(result, item)
		}
	}

	return result
}

func uniqueAndSort(slice []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, item := range slice {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}

	sort.Strings(result)
	return result
}

func differenceSlices(a, b []string) []string {
	m := make(map[string]bool)
	for _, item := range b {
		m[item] = true
	}

	var result []string
	for _, item := range a {
		if !m[item] {
			result = append(result, item)
		}
	}

	return result
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

// getAllPrimaryKeys 獲取collection中所有記錄的主鍵
func (qe *QueryExecutor) getAllPrimaryKeys(collection string) ([]string, error) {
	schema, err := qe.getCollectionSchema(collection)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf("SELECT %s FROM %s", schema.PrimaryKey, collection)
	rows, err := qe.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get all primary keys: %w", err)
	}
	defer rows.Close()

	var primaryKeys []string
	for rows.Next() {
		var pk string
		if err := rows.Scan(&pk); err != nil {
			return nil, fmt.Errorf("failed to scan primary key: %w", err)
		}
		primaryKeys = append(primaryKeys, pk)
	}

	return primaryKeys, nil
}

// fetchRecordsByPrimaryKeys 根據主鍵列表獲取完整記錄
func (qe *QueryExecutor) fetchRecordsByPrimaryKeys(collection, primaryKeyField string, primaryKeys []string, result *ResultSpec) ([]map[string]interface{}, error) {
	if len(primaryKeys) == 0 {
		return []map[string]interface{}{}, nil
	}

	// 構建查詢字段
	fields := "*"
	if len(result.Fields) > 0 {
		// 驗證字段名稱
		for _, field := range result.Fields {
			if !isValidIdentifier(field) && field != "*" {
				return nil, fmt.Errorf("invalid field name: %s", field)
			}
		}
		fields = strings.Join(result.Fields, ", ")
	}

	// 構建WHERE子句
	placeholders := make([]string, len(primaryKeys))
	args := make([]interface{}, len(primaryKeys))
	for i, pk := range primaryKeys {
		placeholders[i] = "?"
		args[i] = pk
	}

	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s IN (%s)", 
		fields, collection, primaryKeyField, strings.Join(placeholders, ", "))

	// 添加ORDER BY
	if len(result.OrderBy) > 0 {
		var orderClauses []string
		for _, order := range result.OrderBy {
			if !isValidIdentifier(order.Field) {
				return nil, fmt.Errorf("invalid order field: %s", order.Field)
			}
			direction := "ASC"
			if strings.ToUpper(order.Direction) == "DESC" {
				direction = "DESC"
			}
			orderClauses = append(orderClauses, order.Field+" "+direction)
		}
		query += " ORDER BY " + strings.Join(orderClauses, ", ")
	}

	// 添加LIMIT和OFFSET
	if result.Limit > 0 {
		query += " LIMIT " + strconv.Itoa(result.Limit)
		if result.Offset > 0 {
			query += " OFFSET " + strconv.Itoa(result.Offset)
		}
	}

	logger.Debug("Fetching records by primary keys", "query", query, "pk_count", len(primaryKeys))

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

	var records []map[string]interface{}
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
		records = append(records, record)
	}

	return records, nil
}

// fetchRecordsByPrimaryKeysWithScores 根據主鍵列表獲取完整記錄，並加入FTS分數，按分數排序
func (qe *QueryExecutor) fetchRecordsByPrimaryKeysWithScores(collection, primaryKeyField string, primaryKeys []string, scoreMap ScoreMap, result *ResultSpec) ([]map[string]interface{}, error) {
	if len(primaryKeys) == 0 {
		return []map[string]interface{}{}, nil
	}

	// 構建查詢字段
	fields := "*"
	if len(result.Fields) > 0 {
		// 驗證字段名稱
		for _, field := range result.Fields {
			if !isValidIdentifier(field) && field != "*" {
				return nil, fmt.Errorf("invalid field name: %s", field)
			}
		}
		fields = strings.Join(result.Fields, ", ")
	}

	// 構建WHERE子句
	placeholders := make([]string, len(primaryKeys))
	args := make([]interface{}, len(primaryKeys))
	for i, pk := range primaryKeys {
		placeholders[i] = "?"
		args[i] = pk
	}

	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s IN (%s)", 
		fields, collection, primaryKeyField, strings.Join(placeholders, ", "))

	logger.Debug("Fetching records by primary keys with scores", "query", query, "pk_count", len(primaryKeys))

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

	var records []map[string]interface{}
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
				record["_score"] = score
			}
		}

		records = append(records, record)
	}

	// 如果有FTS分數，按分數排序（分數越小越相關）
	if len(scoreMap) > 0 {
		sort.Slice(records, func(i, j int) bool {
			scoreI, hasI := records[i]["_score"]
			scoreJ, hasJ := records[j]["_score"]
			
			// 有分數的記錄排在前面
			if hasI && !hasJ {
				return true
			}
			if !hasI && hasJ {
				return false
			}
			
			// 都有分數時，按分數排序（小分數在前）
			if hasI && hasJ {
				if sI, ok := scoreI.(float64); ok {
					if sJ, ok := scoreJ.(float64); ok {
						return sI < sJ
					}
				}
			}
			
			// 都沒有分數或無法比較時，保持原順序
			return false
		})
	}

	// 應用 LIMIT 和 OFFSET（在排序之後）
	if result.Limit > 0 {
		start := result.Offset
		if start < 0 {
			start = 0
		}
		end := start + result.Limit
		if start >= len(records) {
			return []map[string]interface{}{}, nil
		}
		if end > len(records) {
			end = len(records)
		}
		records = records[start:end]
	} else if result.Offset > 0 {
		if result.Offset >= len(records) {
			return []map[string]interface{}{}, nil
		}
		records = records[result.Offset:]
	}

	return records, nil
}