// internal/server/sampling.go
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"mcp-meal-log/internal/models"
)

type SamplingClient struct {
	httpClient  *http.Client
	mcpProxyURL string
	apiKey      string
}

func NewSamplingClient() *SamplingClient {
	return &SamplingClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		mcpProxyURL: "http://mcp-compose-http-proxy:9876", // Internal Docker network
		apiKey:      "myapikey",                           // Should match your mcp-compose config
	}
}

// CalculateCarbs uses the sequentialthinking MCP server via mcp-compose proxy
func (s *SamplingClient) CalculateCarbs(ctx context.Context, req *models.CarbCalculationRequest) (*models.CarbCalculationResponse, error) {
	// Create a detailed prompt for carb calculation using sequential thinking
	promptData := map[string]interface{}{
		"thought": fmt.Sprintf(`I need to analyze this meal description and calculate carbohydrates: "%s"

Let me break this down step by step:
1. Identify each food item in the description
2. Estimate portion sizes based on typical serving descriptions
3. Look up approximate carbohydrate content per food item
4. Calculate total carbohydrates
5. Assess confidence level based on description detail
6. Identify any clarifying questions needed

Starting my analysis...`, req.MealDescription),
		"nextThoughtNeeded": true,
		"thoughtNumber":     1,
		"totalThoughts":     8,
	}

	// Call the sequential thinking tool through mcp-compose proxy
	toolURL := fmt.Sprintf("%s/sequentialthinking", s.mcpProxyURL)

	jsonData, err := json.Marshal(promptData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", toolURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sequential thinking request failed with status %d", resp.StatusCode)
	}

	var thinkingResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&thinkingResp); err != nil {
		return nil, fmt.Errorf("failed to decode thinking response: %w", err)
	}

	// Extract the final analysis and parse it into our response format
	return s.parseThinkingResponse(thinkingResp, req.MealDescription)
}

func (s *SamplingClient) parseThinkingResponse(thinkingResp map[string]interface{}, mealDesc string) (*models.CarbCalculationResponse, error) {
	// For now, provide a simplified response structure
	// In a full implementation, you would parse the sequential thinking output
	// and extract structured food/carb data

	foods := []models.Food{
		{
			Name:           "Parsed from: " + mealDesc,
			Quantity:       "estimated",
			CarbsPer100g:   25.0,                    // Example value
			EstimatedCarbs: 15.0,                    // Example value
			Confidence:     models.MediumConfidence, // Fixed: removed string() conversion
		},
	}

	response := &models.CarbCalculationResponse{
		Foods:         foods,
		TotalCarbs:    15.0, // Sum of estimated carbs
		Confidence:    models.MediumConfidence,
		NeedsMoreInfo: len(mealDesc) < 20, // Simple heuristic
	}

	// Add clarifications if description is too vague
	if response.NeedsMoreInfo {
		response.Clarifications = []string{
			"What was the approximate portion size?",
			"How was the food prepared (fried, baked, etc.)?",
			"Were there any sauces or condiments?",
		}
	}

	return response, nil
}

// AskClarification handles follow-up questions for more accurate carb calculation
func (s *SamplingClient) AskClarification(ctx context.Context, mealDesc string, questions []string) (*models.CarbCalculationResponse, error) {
	// Similar implementation using sequential thinking for clarifications
	// This would involve a more complex prompt that includes the original description
	// and the clarifying questions/answers

	log.Printf("Processing clarifications for meal: %s", mealDesc)
	log.Printf("Questions: %v", questions)

	// For now, return a simple response
	return s.CalculateCarbs(ctx, &models.CarbCalculationRequest{
		MealDescription:   mealDesc,
		AskClarifications: false,
	})
}
