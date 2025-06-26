// internal/models/meal.go
package models

import (
    "time"
)

type Meal struct {
    ID          string             `json:"id"`
    Description string             `json:"description"`
    Timestamp   time.Time          `json:"timestamp"`
    Foods       []Food             `json:"foods"`
    TotalCarbs  float64            `json:"total_carbs"`
    Confidence  ConfidenceLevel    `json:"confidence"`
    CreatedAt   time.Time          `json:"created_at"`
    UpdatedAt   time.Time          `json:"updated_at"`
    Source      string             `json:"source"` // "manual", "ai_parsed"
}

type Food struct {
    Name           string          `json:"name"`
    Quantity       string          `json:"quantity"`
    CarbsPer100g   float64         `json:"carbs_per_100g"`
    EstimatedCarbs float64         `json:"estimated_carbs"`
    Confidence     ConfidenceLevel `json:"confidence"`
}

type ConfidenceLevel string

const (
    HighConfidence   ConfidenceLevel = "high"
    MediumConfidence ConfidenceLevel = "medium"
    LowConfidence    ConfidenceLevel = "low"
)

type CarbCalculationRequest struct {
    MealDescription   string `json:"meal_description"`
    AskClarifications bool   `json:"ask_clarifications"`
}

type CarbCalculationResponse struct {
    Foods          []Food          `json:"foods"`
    TotalCarbs     float64         `json:"total_carbs"`
    Confidence     ConfidenceLevel `json:"confidence"`
    Clarifications []string        `json:"clarifications,omitempty"`
    NeedsMoreInfo  bool            `json:"needs_more_info"`
}
