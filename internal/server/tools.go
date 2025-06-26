// internal/server/tools.go
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"

	"mcp-meal-log/internal/models"
)

type LogMealParams struct {
	Description string `json:"description" description:"Description of the meal eaten"`
	Timestamp   string `json:"timestamp,omitempty" description:"ISO timestamp of when meal was eaten (defaults to now)"`
}

type CalculateCarbsParams struct {
	MealDescription   string `json:"meal_description" description:"Description of the meal to analyze"`
	AskClarifications bool   `json:"ask_clarifications" description:"Whether to ask clarifying questions if needed"`
}

type GetMealsParams struct {
	StartDate string `json:"start_date,omitempty" description:"Start date for meal query (YYYY-MM-DD)"`
	EndDate   string `json:"end_date,omitempty" description:"End date for meal query (YYYY-MM-DD)"`
	Limit     int    `json:"limit,omitempty" description:"Maximum number of meals to return"`
}

// extractParams safely extracts parameters from the request arguments
func extractParams(req *protocol.CallToolRequest, target interface{}) error {
	// Convert the Arguments map to JSON bytes, then unmarshal to target
	jsonBytes, err := json.Marshal(req.Arguments)
	if err != nil {
		return fmt.Errorf("failed to marshal arguments: %w", err)
	}

	if err := json.Unmarshal(jsonBytes, target); err != nil {
		return fmt.Errorf("failed to unmarshal parameters: %w", err)
	}

	return nil
}

// handleLogMeal processes meal logging with AI-powered carb calculation
func (s *MealLogServer) handleLogMeal(req *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	var params LogMealParams
	if err := extractParams(req, &params); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	if params.Description == "" {
		return nil, fmt.Errorf("meal description is required")
	}

	// Parse timestamp or use current time
	var timestamp time.Time
	var err error
	if params.Timestamp != "" {
		timestamp, err = time.Parse(time.RFC3339, params.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("invalid timestamp format: %w", err)
		}
	} else {
		timestamp = time.Now()
	}

	// Use AI to calculate carbs
	carbReq := &models.CarbCalculationRequest{
		MealDescription:   params.Description,
		AskClarifications: true,
	}

	carbResp, err := s.samplingClient.CalculateCarbs(context.Background(), carbReq)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate carbs: %w", err)
	}

	// If clarifications are needed, return them instead of logging
	if carbResp.NeedsMoreInfo && len(carbResp.Clarifications) > 0 {
		result := map[string]interface{}{
			"needs_clarification":  true,
			"clarifications":       carbResp.Clarifications,
			"preliminary_analysis": carbResp,
		}
		return s.createJSONResponse(result)
	}

	// Create meal entry
	meal := &models.Meal{
		ID:          fmt.Sprintf("meal_%d", time.Now().UnixNano()),
		Description: params.Description,
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

	return s.createJSONResponse(meal)
}

// handleCalculateCarbs calculates carbs without logging the meal
func (s *MealLogServer) handleCalculateCarbs(req *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	var params CalculateCarbsParams
	if err := extractParams(req, &params); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	if params.MealDescription == "" {
		return nil, fmt.Errorf("meal description is required")
	}

	carbReq := &models.CarbCalculationRequest{
		MealDescription:   params.MealDescription,
		AskClarifications: params.AskClarifications,
	}

	result, err := s.samplingClient.CalculateCarbs(context.Background(), carbReq)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate carbs: %w", err)
	}

	return s.createJSONResponse(result)
}

// handleGetMeals retrieves meals from storage
func (s *MealLogServer) handleGetMeals(req *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	var params GetMealsParams
	if err := extractParams(req, &params); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	// Set defaults
	if params.Limit <= 0 {
		params.Limit = 20
	}

	meals, err := s.storage.GetMeals(params.StartDate, params.EndDate, params.Limit)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve meals: %w", err)
	}

	return s.createJSONResponse(meals)
}

// addMealToKnowledgeGraph integrates with your existing knowledge graph system
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

// Register all tools - simplified without protocol.NewTool
func (s *MealLogServer) registerTools() error {
	// Since we're handling tools manually in the HTTP handler,
	// this is just for validation that our tool handlers exist
	tools := map[string]func(*protocol.CallToolRequest) (*protocol.CallToolResult, error){
		"log_meal":        s.handleLogMeal,
		"calculate_carbs": s.handleCalculateCarbs,
		"get_meals":       s.handleGetMeals,
	}

	// Just verify all handlers are present
	for name := range tools {
		fmt.Printf("Registered tool: %s\n", name)
	}

	return nil
}
