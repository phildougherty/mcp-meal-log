// internal/server/sampling.go - Add model configuration
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"mcp-meal-log/internal/models"
)

type SamplingClient struct {
	httpClient *http.Client
	proxyURL   string
	apiKey     string
	model      string // Add model field
}

func NewSamplingClient() *SamplingClient {
	proxyURL := os.Getenv("MCP_PROXY_URL")
	if proxyURL == "" {
		proxyURL = "http://mcp-compose-http-proxy:9876"
	}

	apiKey := os.Getenv("MCP_PROXY_API_KEY")
	if apiKey == "" {
		apiKey = "myapikey"
	}

	// Get model from environment variable
	model := os.Getenv("OPENROUTER_MODEL")
	if model == "" {
		model = "anthropic/claude-3.5-sonnet" // Default fallback
	}

	return &SamplingClient{
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		proxyURL: proxyURL,
		apiKey:   apiKey,
		model:    model, // Store the model
	}
}

func (s *SamplingClient) CalculateCarbs(ctx context.Context, req *models.CarbCalculationRequest) (*models.CarbCalculationResponse, error) {
	// Create a specialized prompt for carb analysis
	systemPrompt := `You are a nutrition expert specializing in carbohydrate counting for diabetes management. 

When analyzing meals, provide accurate carbohydrate estimates and identify when more information is needed.

IMPORTANT: Always respond with valid JSON in this exact format:
{
  "foods": [
    {
      "name": "specific food item name",
      "quantity": "estimated portion size with units", 
      "carbs_per_100g": [number],
      "estimated_carbs": [number],
      "confidence": "high|medium|low"
    }
  ],
  "total_carbs": [number],
  "confidence": "high|medium|low",
  "clarifications": ["specific question1", "specific question2"],
  "needs_more_info": [true/false]
}

For items like "a baked potato", ask specific questions about size since this greatly affects carbohydrate content.`

	clarificationText := ""
	if req.AskClarifications {
		clarificationText = `

If the description lacks specific details about:
- Portion sizes (small, medium, large, or specific measurements)
- Preparation methods that affect carbs
- Specific varieties that have different carb contents

Then set "needs_more_info" to true and include specific clarifying questions in the "clarifications" array.`
	}

	userPrompt := fmt.Sprintf(`Analyze this meal and calculate carbohydrates: "%s"

Provide detailed breakdown of each food item, realistic portion estimates, and total carbohydrates.%s`, req.MealDescription, clarificationText)

	// Call the OpenRouter gateway using the configured model
	completionRequest := map[string]interface{}{
		"model":         s.model, // Use the configured model
		"system_prompt": systemPrompt,
		"messages": []map[string]interface{}{
			{
				"role":    "user",
				"content": userPrompt,
			},
		},
		"max_tokens":  2000,
		"temperature": 0.1, // Low temperature for consistent analysis
	}

	// Call the gateway
	gatewayResponse, err := s.callGateway("create_completion", completionRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to get AI completion: %w", err)
	}

	// Parse the AI response
	return s.parseAIResponse(gatewayResponse)
}

// Rest of the methods remain the same...
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
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("request failed with status %d and couldn't read body: %v", resp.StatusCode, err)
		}
		return "", fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
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
					return text, nil
				}
			}
		}
	}

	return "", fmt.Errorf("unexpected response format")
}

func (s *SamplingClient) parseAIResponse(aiOutput string) (*models.CarbCalculationResponse, error) {
	// Parse the completion response
	var completionResp map[string]interface{}
	if err := json.Unmarshal([]byte(aiOutput), &completionResp); err != nil {
		return s.createFallbackResponse(aiOutput), nil
	}

	// Get the content from the completion
	content, ok := completionResp["content"].(string)
	if !ok {
		return s.createFallbackResponse(aiOutput), nil
	}

	// Extract JSON from the content
	start := strings.Index(content, "{")
	if start == -1 {
		return s.createFallbackResponse(content), nil
	}

	end := strings.LastIndex(content, "}")
	if end == -1 || end <= start {
		return s.createFallbackResponse(content), nil
	}

	jsonStr := content[start : end+1]

	var response models.CarbCalculationResponse
	if err := json.Unmarshal([]byte(jsonStr), &response); err != nil {
		return s.createFallbackResponse(content), nil
	}

	return &response, nil
}

func (s *SamplingClient) createFallbackResponse(aiOutput string) *models.CarbCalculationResponse {
	return &models.CarbCalculationResponse{
		Foods: []models.Food{
			{
				Name:           "Analysis unavailable",
				Quantity:       "unknown",
				CarbsPer100g:   0,
				EstimatedCarbs: 20.0,
				Confidence:     models.LowConfidence,
			},
		},
		TotalCarbs:    20.0,
		Confidence:    models.LowConfidence,
		NeedsMoreInfo: true,
		Clarifications: []string{
			"What size was the potato (small, medium, large)?",
			"How much sour cream was used?",
			"Were there any other toppings?",
		},
	}
}

func (s *SamplingClient) AskClarification(ctx context.Context, mealDesc string, questions []string) (*models.CarbCalculationResponse, error) {
	return s.CalculateCarbs(ctx, &models.CarbCalculationRequest{
		MealDescription:   mealDesc,
		AskClarifications: false,
	})
}
