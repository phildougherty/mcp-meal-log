// internal/server/tools.go
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mcp-meal-log/internal/models"
)

type LogMealParams struct {
	Description string `json:"description"`
	Timestamp   string `json:"timestamp,omitempty"`
}

type CalculateCarbsParams struct {
	MealDescription   string `json:"meal_description"`
	AskClarifications bool   `json:"ask_clarifications"`
}

type GetMealsParams struct {
	StartDate string `json:"start_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

// helper function to convert map to struct
func mapToStruct(data map[string]interface{}, target interface{}) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(jsonBytes, target)
}

func (s *MealLogServer) logMeal(params map[string]interface{}) (interface{}, error) {
	var p LogMealParams
	if err := mapToStruct(params, &p); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	if p.Description == "" {
		return nil, fmt.Errorf("meal description is required")
	}

	// Parse timestamp or use current time
	var timestamp time.Time
	var err error
	if p.Timestamp != "" {
		timestamp, err = time.Parse(time.RFC3339, p.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("invalid timestamp format: %w", err)
		}
	} else {
		timestamp = time.Now()
	}

	// Use AI to calculate carbs
	carbReq := &models.CarbCalculationRequest{
		MealDescription:   p.Description,
		AskClarifications: true,
	}

	carbResp, err := s.samplingClient.CalculateCarbs(context.Background(), carbReq)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate carbs: %w", err)
	}

	// If clarifications are needed, return them instead of logging
	if carbResp.NeedsMoreInfo && len(carbResp.Clarifications) > 0 {
		return map[string]interface{}{
			"needs_clarification":  true,
			"clarifications":       carbResp.Clarifications,
			"preliminary_analysis": carbResp,
		}, nil
	}

	// Create meal entry
	meal := &models.Meal{
		ID:          fmt.Sprintf("meal_%d", time.Now().UnixNano()),
		Description: p.Description,
		Timestamp:   timestamp,
		Foods:       carbResp.Foods,
		TotalCarbs:  carbResp.TotalCarbs,
		Confidence:  carbResp.Confidence,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Source:      "ai_parsed",
	}

	// Save to storage
	if err := s.storage.SaveMeal(meal); err != nil {
		return nil, fmt.Errorf("failed to save meal: %w", err)
	}

	// Add to knowledge graph via memory MCP server
	if err := s.addMealToKnowledgeGraph(meal); err != nil {
		// Don't fail the whole operation, just log the warning
		fmt.Printf("Warning: failed to add meal to knowledge graph: %v\n", err)
	}

	return meal, nil
}

func (s *MealLogServer) calculateCarbs(params map[string]interface{}) (interface{}, error) {
	var p CalculateCarbsParams
	if err := mapToStruct(params, &p); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	if p.MealDescription == "" {
		return nil, fmt.Errorf("meal description is required")
	}

	carbReq := &models.CarbCalculationRequest{
		MealDescription:   p.MealDescription,
		AskClarifications: p.AskClarifications,
	}

	result, err := s.samplingClient.CalculateCarbs(context.Background(), carbReq)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate carbs: %w", err)
	}

	return result, nil
}

func (s *MealLogServer) getMeals(params map[string]interface{}) (interface{}, error) {
	var p GetMealsParams
	if err := mapToStruct(params, &p); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	// Set defaults
	if p.Limit <= 0 {
		p.Limit = 20
	}

	meals, err := s.storage.GetMeals(p.StartDate, p.EndDate, p.Limit)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve meals: %w", err)
	}

	return meals, nil
}

func (s *MealLogServer) addMealToKnowledgeGraph(meal *models.Meal) error {
	// Call the memory MCP server via mcp-compose proxy to create entities
	entityData := map[string]interface{}{
		"entities": []map[string]interface{}{
			{
				"name":       fmt.Sprintf("Meal_%s", meal.Timestamp.Format("2006-01-02_15-04")),
				"entityType": "Meal Entry",
				"observations": []string{
					fmt.Sprintf("Description: %s", meal.Description),
					fmt.Sprintf("Total Carbs: %.1f g", meal.TotalCarbs),
					fmt.Sprintf("Timestamp: %s", meal.Timestamp.Format(time.RFC3339)),
					fmt.Sprintf("Confidence: %s", meal.Confidence),
					fmt.Sprintf("Foods: %s", s.formatFoodsList(meal.Foods)),
					fmt.Sprintf("Source: %s", meal.Source),
				},
			},
		},
	}

	// Call memory server through mcp-compose proxy
	return s.callMemoryService("create_entities", entityData)
}

func (s *MealLogServer) formatFoodsList(foods []models.Food) string {
	var foodStrings []string
	for _, food := range foods {
		foodStrings = append(foodStrings, fmt.Sprintf("%s (%s, %.1fg carbs)",
			food.Name, food.Quantity, food.EstimatedCarbs))
	}
	return strings.Join(foodStrings, "; ")
}

func (s *MealLogServer) callMemoryService(toolName string, data interface{}) error {
	// Implementation to call memory MCP server via proxy
	// This would make HTTP requests to the memory service
	fmt.Printf("Would call memory service %s with data: %+v\n", toolName, data)
	return nil // Placeholder
}
