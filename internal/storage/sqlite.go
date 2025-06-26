// internal/storage/sqlite.go
package storage

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"mcp-meal-log/internal/models"
)

type SQLiteStorage struct {
	db *sql.DB
}

func NewSQLiteStorage(dbPath string) (*SQLiteStorage, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	storage := &SQLiteStorage{db: db}
	if err := storage.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return storage, nil
}

func (s *SQLiteStorage) Close() error {
	return s.db.Close()
}

func (s *SQLiteStorage) initSchema() error {
	schema := `
    CREATE TABLE IF NOT EXISTS meals (
        id TEXT PRIMARY KEY,
        description TEXT NOT NULL,
        timestamp DATETIME NOT NULL,
        total_carbs REAL NOT NULL,
        confidence TEXT NOT NULL,
        created_at DATETIME NOT NULL,
        updated_at DATETIME NOT NULL,
        source TEXT NOT NULL
    );

    CREATE TABLE IF NOT EXISTS foods (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        meal_id TEXT NOT NULL,
        name TEXT NOT NULL,
        quantity TEXT NOT NULL,
        carbs_per_100g REAL NOT NULL,
        estimated_carbs REAL NOT NULL,
        confidence TEXT NOT NULL,
        FOREIGN KEY (meal_id) REFERENCES meals(id) ON DELETE CASCADE
    );

    CREATE INDEX IF NOT EXISTS idx_meals_timestamp ON meals(timestamp);
    CREATE INDEX IF NOT EXISTS idx_foods_meal_id ON foods(meal_id);
    `

	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}

func (s *SQLiteStorage) SaveMeal(meal *models.Meal) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	// Insert meal
	mealQuery := `
        INSERT INTO meals (id, description, timestamp, total_carbs, confidence, created_at, updated_at, source)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `
	_, err = tx.Exec(mealQuery,
		meal.ID, meal.Description, meal.Timestamp, meal.TotalCarbs,
		string(meal.Confidence), meal.CreatedAt, meal.UpdatedAt, meal.Source)
	if err != nil {
		return fmt.Errorf("failed to insert meal: %w", err)
	}

	// Insert foods
	foodQuery := `
        INSERT INTO foods (meal_id, name, quantity, carbs_per_100g, estimated_carbs, confidence)
        VALUES (?, ?, ?, ?, ?, ?)
    `
	for _, food := range meal.Foods {
		_, err = tx.Exec(foodQuery,
			meal.ID, food.Name, food.Quantity, food.CarbsPer100g,
			food.EstimatedCarbs, string(food.Confidence))
		if err != nil {
			return fmt.Errorf("failed to insert food: %w", err)
		}
	}

	return tx.Commit()
}

func (s *SQLiteStorage) GetMeals(startDate, endDate string, limit int) ([]*models.Meal, error) {
	query := `
        SELECT id, description, timestamp, total_carbs, confidence, created_at, updated_at, source
        FROM meals
        WHERE 1=1
    `
	args := []interface{}{}

	if startDate != "" {
		query += " AND DATE(timestamp) >= ?"
		args = append(args, startDate)
	}
	if endDate != "" {
		query += " AND DATE(timestamp) <= ?"
		args = append(args, endDate)
	}

	query += " ORDER BY timestamp DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query meals: %w", err)
	}
	defer rows.Close()

	var meals []*models.Meal
	for rows.Next() {
		meal := &models.Meal{}
		var timestampStr, createdAtStr, updatedAtStr string
		var confidenceStr string

		err := rows.Scan(
			&meal.ID, &meal.Description, &timestampStr, &meal.TotalCarbs,
			&confidenceStr, &createdAtStr, &updatedAtStr, &meal.Source)
		if err != nil {
			return nil, fmt.Errorf("failed to scan meal: %w", err)
		}

		// Parse timestamps
		if meal.Timestamp, err = time.Parse(time.RFC3339, timestampStr); err != nil {
			return nil, fmt.Errorf("failed to parse timestamp: %w", err)
		}
		if meal.CreatedAt, err = time.Parse(time.RFC3339, createdAtStr); err != nil {
			return nil, fmt.Errorf("failed to parse created_at: %w", err)
		}
		if meal.UpdatedAt, err = time.Parse(time.RFC3339, updatedAtStr); err != nil {
			return nil, fmt.Errorf("failed to parse updated_at: %w", err)
		}

		meal.Confidence = models.ConfidenceLevel(confidenceStr)

		// Load foods for this meal
		if err := s.loadFoodsForMeal(meal); err != nil {
			return nil, fmt.Errorf("failed to load foods for meal %s: %w", meal.ID, err)
		}

		meals = append(meals, meal)
	}

	return meals, nil
}

func (s *SQLiteStorage) loadFoodsForMeal(meal *models.Meal) error {
	query := `
        SELECT name, quantity, carbs_per_100g, estimated_carbs, confidence
        FROM foods
        WHERE meal_id = ?
        ORDER BY id
    `

	rows, err := s.db.Query(query, meal.ID)
	if err != nil {
		return fmt.Errorf("failed to query foods: %w", err)
	}
	defer rows.Close()

	var foods []models.Food
	for rows.Next() {
		food := models.Food{}
		var confidenceStr string

		err := rows.Scan(
			&food.Name, &food.Quantity, &food.CarbsPer100g,
			&food.EstimatedCarbs, &confidenceStr)
		if err != nil {
			return fmt.Errorf("failed to scan food: %w", err)
		}

		food.Confidence = models.ConfidenceLevel(confidenceStr)
		foods = append(foods, food)
	}

	meal.Foods = foods
	return nil
}
