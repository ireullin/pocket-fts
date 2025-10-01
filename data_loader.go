package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	BASE_URL = "http://localhost:5122"
)

type CollectionSchema struct {
	Name       string    `json:"name"`
	PrimaryKey string    `json:"primary_key"`
	FTS        FTSConfig `json:"fts"`
	Fields     []Field   `json:"fields"`
}

type Field struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Weight int    `json:"weight,omitempty"`
}

type FTSConfig struct {
	Stemming bool `json:"stemming"`
}

type DocumentUpsert struct {
	Collection string                 `json:"collection"`
	Document   map[string]interface{} `json:"document"`
}

func main() {
	fmt.Println("=== 資料載入程式 ===")

	// 設置隨機種子
	rand.Seed(time.Now().UnixNano())

	// 1. 清理並建立 collection
	fmt.Println("清理舊的 collection...")
	deleteCollection() // 忽略錯誤，因為可能不存在

	fmt.Println("建立 collection...")
	if err := createCollection(); err != nil {
		log.Fatalf("建立 collection 失敗: %v", err)
	}
	fmt.Println("✓ Collection 建立成功")

	// 2. 載入 TSV 資料
	fmt.Println("載入 TSV 資料...")
	if err := loadTSVData("simple.tsv"); err != nil {
		log.Fatalf("載入資料失敗: %v", err)
	}
	fmt.Println("✓ 資料載入完成")
}

func createCollection() error {
	schema := CollectionSchema{
		Name:       "products",
		PrimaryKey: "id",
		FTS: FTSConfig{
			Stemming: true,
		},
		Fields: []Field{
			{Name: "id", Type: "text"},
			{Name: "type", Type: "text", Weight: 1},
			{Name: "category", Type: "text", Weight: 2},
			{Name: "item", Type: "text", Weight: 3},
			{Name: "price", Type: "integer"},
			{Name: "weight", Type: "real"},
		},
	}

	jsonData, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("序列化 schema 失敗: %w", err)
	}

	resp, err := http.Post(BASE_URL+"/collections/create", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("HTTP 請求失敗: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("建立 collection 失敗，狀態碼: %d", resp.StatusCode)
	}

	return nil
}

func deleteCollection() error {
	deleteReq := map[string]string{"name": "products"}
	jsonData, _ := json.Marshal(deleteReq)

	resp, err := http.Post(BASE_URL+"/collections/delete", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func loadTSVData(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("開啟檔案失敗: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0
	successCount := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// 解析 TSV 行
		parts := strings.Split(line, "\t")
		if len(parts) < 4 {
			fmt.Printf("警告: 第 %d 行格式不正確，跳過: %s\n", lineNum, line)
			continue
		}

		// 提取欄位 (忽略第三欄)
		typeField := strings.TrimSpace(parts[0])
		category := strings.TrimSpace(parts[1])
		// parts[2] 被忽略
		item := strings.TrimSpace(parts[3])

		// 跳過空的記錄
		if typeField == "" || item == "" {
			fmt.Printf("警告: 第 %d 行有空欄位，跳過\n", lineNum)
			continue
		}

		// 生成隨機價格和重量
		price := generateRandomPrice()
		weight := generateRandomWeight()

		// 建立文檔
		document := map[string]interface{}{
			"id":       fmt.Sprintf("item_%d", lineNum),
			"type":     typeField,
			"category": category,
			"item":     item,
			"price":    price,
			"weight":   weight,
		}

		// 上傳文檔
		if err := upsertDocument(document); err != nil {
			fmt.Printf("警告: 第 %d 行上傳失敗: %v\n", lineNum, err)
			continue
		}

		successCount++
		if successCount%50 == 0 {
			fmt.Printf("已處理 %d 筆資料...\n", successCount)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("讀取檔案錯誤: %w", err)
	}

	fmt.Printf("總計處理 %d 行，成功載入 %d 筆資料\n", lineNum, successCount)
	return nil
}

func upsertDocument(document map[string]interface{}) error {
	upsertReq := DocumentUpsert{
		Collection: "products",
		Document:   document,
	}

	jsonData, err := json.Marshal(upsertReq)
	if err != nil {
		return fmt.Errorf("序列化文檔失敗: %w", err)
	}

	resp, err := http.Post(BASE_URL+"/documents/upsert", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("HTTP 請求失敗: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("上傳文檔失敗，狀態碼: %d", resp.StatusCode)
	}

	return nil
}

// 生成隨機價格 (100-50000 之間)
func generateRandomPrice() int {
	return rand.Intn(49901) + 100
}

// 生成隨機重量 (100-5000 之間，以克為單位)
func generateRandomWeight() int {
	return rand.Intn(4901) + 100
}
