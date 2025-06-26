// internal/server/sampling.go - Updated for meal-log server
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"mcp-meal-log/internal/models"
)

type SamplingClient struct {
	httpClient *http.Client
	gatewayURL string
	proxyURL   string
	apiKey     string
}

func NewSamplingClient() *SamplingClient {
	return &SamplingClient{
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		gatewayURL: "http://mcp-compose-openrouter-gateway:8012",
		proxyURL:   "http://mcp-compose-http-proxy:9876",
		apiKey:     "myapikey",
	}
}

func (s *SamplingClient) CalculateCarbs(ctx context.Context, req *models.CarbCalculationRequest) (*models.CarbCalculationResponse, error) {
	// Create a specialized prompt for carb analysis
	systemPrompt := `You are a nutrition expert specializing in carbohydrate counting for diabetes management. 

When analyzing meals, provide accurate carbohydrate estimates and identify when more information is needed.

Always respond with valid JSON in this exact format:
{
  "foods": [
    {
      "name": "food item name", 
      "quantity": "estimated portion size",
      "carbs_per_100g": [number],
      "estimated_carbs": [number],
      "confidence": "high|medium|low"
    }
  ],
  "total_carbs": [number],
  "confidence": "high|medium|low", 
  "clarifications": ["question1", "question2"],
  "needs_more_info": [true/false]
}`

	userPrompt := fmt.Sprintf(`Analyze this meal and calculate carbohydrates: "%s"

Provide detailed breakdown of each food item, portion estimates, and total carbohydrates.

%s`, req.MealDescription, func() string {
		if req.AskClarifications {
			return "If the description is vague, include specific clarifying questions in the 'clarifications' array and set 'needs_more_info' to true."
		}
		return "Provide your best estimate even if some details are unclear."
	}())

	// Call the generic OpenRouter gateway
	completionRequest := map[string]interface{}{
		"model":         "anthropic/claude-3.5-sonnet",
		"system_prompt": systemPrompt,
		"messages": []map[string]interface{}{
			{
				"role":    "user",
				"content": userPrompt,
			},
		},
		"max_tokens":  1500,
		"temperature": 0.1, // Low temperature for consistent analysis
	}

	// Call the gateway via mcp-compose proxy
	gatewayResponse, err := s.callGateway("create_completion", completionRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to get AI completion: %w", err)
	}

	// Parse the AI response
	return s.parseAIResponse(gatewayResponse)
}

func (s *SamplingClient) callGateway(toolName string, args interface{}) (string, error) {
	url := fmt.Sprintf("%s/openrouter-gateway", s.proxyURL)

	requestData := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": args,
		},
	}

	jsonData, err := json.Marshal(requestData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("request failed with status %d", resp.StatusCode)
	}

	var mcpResponse map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&mcpResponse); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract the result content
	if result, ok := mcpResponse["result"].(map[string]interface{}); ok {
		if content, ok := result["content"].([]interface{}); ok && len(content) > 0 {
			if textContent, ok := content[0].(map[string]interface{}); ok {
				if text, ok := textContent["text"].(string); ok {
					// The text should contain the completion response JSON
					return text, nil
				}
			}
		}
	}

	return "", fmt.Errorf("unexpected response format")
}

func (s *SamplingClient) parseAIResponse(aiOutput string) (*models.CarbCalculationResponse, error) {
	// Extract the completion response first
	var completionResp map[string]interface{}
	if err := json.Unmarshal([]byte(aiOutput), &completionResp); err != nil {
		return nil, fmt.Errorf("failed to parse completion response: %w", err)
	}

	// Get the content from the completion
	content, ok := completionResp["content"].(string)
	if !ok {
		return nil, fmt.Errorf("no content in completion response")
	}

	// Now parse the actual carb calculation JSON from the content
	var response models.CarbCalculationResponse

	// Try to find JSON in the content
	start := strings.Index(content, "{")
	if start == -1 {
		return s.createFallbackResponse(content), nil
	}

	end := strings.LastIndex(content, "}")
	if end == -1 || end <= start {
		return s.createFallbackResponse(content), nil
	}

	jsonStr := content[start : end+1]

	if err := json.Unmarshal([]byte(jsonStr), &response); err != nil {
		return s.createFallbackResponse(content), nil
	}

	return &response, nil
}

func (s *SamplingClient) createFallbackResponse(aiOutput string) *models.CarbCalculationResponse {
	estimatedCarbs := 20.0 // Default fallback

	return &models.CarbCalculationResponse{
		Foods: []models.Food{
			{
				Name:           "AI Analysis Result",
				Quantity:       "estimated",
				CarbsPer100g:   0,
				EstimatedCarbs: estimatedCarbs,
				Confidence:     models.LowConfidence,
			},
		},
		TotalCarbs:    estimatedCarbs,
		Confidence:    models.LowConfidence,
		NeedsMoreInfo: true,
		Clarifications: []string{
			"Could you provide more specific details about portion sizes?",
			"How was the food prepared (grilled, fried, etc.)?",
			"Were there any sauces or condiments?",
		},
	}
}

func (s *SamplingClient) AskClarification(ctx context.Context, mealDesc string, questions []string) (*models.CarbCalculationResponse, error) {
	return s.CalculateCarbs(ctx, &models.CarbCalculationRequest{
		MealDescription:   mealDesc,
		AskClarifications: false,
	})
}
