// internal/server/server.go
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"mcp-meal-log/internal/storage"
)

type Config struct {
	Transport string
	Host      string
	Port      int
	DBPath    string
}

type MealLogServer struct {
	httpServer     *http.Server
	storage        *storage.SQLiteStorage
	samplingClient *SamplingClient
	config         *Config
}

// MCP Protocol types
type MCPRequest struct {
	Jsonrpc string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type MCPResponse struct {
	Jsonrpc string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type ServerInfo struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocolVersion"`
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}

func NewMealLogServer(cfg *Config) (*MealLogServer, error) {
	// Initialize database
	stor, err := storage.NewSQLiteStorage(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize storage: %w", err)
	}

	mealServer := &MealLogServer{
		storage:        stor,
		samplingClient: NewSamplingClient(),
		config:         cfg,
	}

	// Set up HTTP handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/", mealServer.handleMCP)

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	mealServer.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	log.Printf("Meal log server configured on %s", addr)
	return mealServer, nil
}

func (s *MealLogServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	// Handle CORS
	s.setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		return
	}

	if r.Method != http.MethodPost {
		s.sendMCPError(w, nil, -32601, "Method not allowed")
		return
	}

	// Decode the MCP request
	var request MCPRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		s.sendMCPError(w, nil, -32700, "Parse error")
		return
	}

	// Route to appropriate handler based on method
	var result interface{}
	var err error

	switch request.Method {
	case "initialize":
		result = s.handleInitialize(request.Params)
	case "tools/list":
		result = s.handleToolsList()
	case "tools/call":
		result, err = s.handleToolsCall(request.Params)
	default:
		s.sendMCPError(w, request.ID, -32601, fmt.Sprintf("Unknown method: %s", request.Method))
		return
	}

	if err != nil {
		s.sendMCPError(w, request.ID, -32603, err.Error())
		return
	}

	// Send success response
	response := MCPResponse{
		Jsonrpc: "2.0",
		ID:      request.ID,
		Result:  result,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *MealLogServer) handleInitialize(params interface{}) interface{} {
	return map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
		"serverInfo": ServerInfo{
			Name:            "meal-log",
			Version:         "1.0.0",
			ProtocolVersion: "2024-11-05",
		},
	}
}

func (s *MealLogServer) handleToolsList() interface{} {
	tools := []Tool{
		{
			Name:        "log_meal",
			Description: "Log a meal with automatic carbohydrate calculation using AI",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Description of the meal eaten",
					},
					"timestamp": map[string]interface{}{
						"type":        "string",
						"description": "ISO timestamp of when meal was eaten (defaults to now)",
					},
				},
				"required": []string{"description"},
			},
		},
		{
			Name:        "calculate_carbs",
			Description: "Calculate carbohydrates for a meal description without logging",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"meal_description": map[string]interface{}{
						"type":        "string",
						"description": "Description of the meal to analyze",
					},
					"ask_clarifications": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to ask clarifying questions if needed",
					},
				},
				"required": []string{"meal_description"},
			},
		},
		{
			Name:        "get_meals",
			Description: "Retrieve logged meals within a date range",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"start_date": map[string]interface{}{
						"type":        "string",
						"description": "Start date for meal query (YYYY-MM-DD)",
					},
					"end_date": map[string]interface{}{
						"type":        "string",
						"description": "End date for meal query (YYYY-MM-DD)",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of meals to return",
					},
				},
			},
		},
	}

	return ToolsListResult{Tools: tools}
}

func (s *MealLogServer) handleToolsCall(params interface{}) (interface{}, error) {
	// Parse the tool call parameters
	paramsMap, ok := params.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid parameters format")
	}

	toolName, ok := paramsMap["name"].(string)
	if !ok {
		return nil, fmt.Errorf("tool name is required")
	}

	// Get the arguments
	var args map[string]interface{}
	if arguments, exists := paramsMap["arguments"]; exists {
		args, ok = arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments format")
		}
	} else {
		args = make(map[string]interface{})
	}

	// Route to the appropriate tool handler
	switch toolName {
	case "log_meal":
		result, err := s.logMeal(args)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": formatJSON(result),
				},
			},
		}, nil

	case "calculate_carbs":
		result, err := s.calculateCarbs(args)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": formatJSON(result),
				},
			},
		}, nil

	case "get_meals":
		result, err := s.getMeals(args)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": formatJSON(result),
				},
			},
		}, nil

	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}

func formatJSON(data interface{}) string {
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error formatting response: %v", err)
	}
	return string(jsonBytes)
}

func (s *MealLogServer) setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

func (s *MealLogServer) sendMCPError(w http.ResponseWriter, id interface{}, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // MCP errors are still HTTP 200

	response := MCPResponse{
		Jsonrpc: "2.0",
		ID:      id,
		Error: &MCPError{
			Code:    code,
			Message: message,
		},
	}
	json.NewEncoder(w).Encode(response)
}

func (s *MealLogServer) Start(ctx context.Context) error {
	log.Printf("Starting meal log server on %s", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *MealLogServer) Stop() error {
	if s.storage != nil {
		s.storage.Close()
	}
	if s.httpServer != nil {
		return s.httpServer.Shutdown(context.Background())
	}
	return nil
}
