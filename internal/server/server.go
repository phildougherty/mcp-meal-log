// internal/server/server.go
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/ThinkInAIXYZ/go-mcp/server"

	"mcp-meal-log/internal/storage"
)

type Config struct {
	Transport string
	Host      string
	Port      int
	DBPath    string
}

type MealLogServer struct {
	server         *server.Server
	httpServer     *http.Server
	storage        *storage.SQLiteStorage
	samplingClient *SamplingClient
	config         *Config
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

	// Create HTTP server with MCP handler
	mux := http.NewServeMux()

	// Create MCP server (without transport, we'll handle HTTP manually)
	mcpServer, err := server.NewServer(
		nil, // We'll handle transport manually
		server.WithServerInfo(protocol.Implementation{
			Name:    "meal-log",
			Version: "1.0.0",
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create MCP server: %w", err)
	}

	mealServer.server = mcpServer

	// Register tools
	if err := mealServer.registerTools(); err != nil {
		return nil, fmt.Errorf("failed to register tools: %w", err)
	}

	// Set up HTTP handlers
	mux.HandleFunc("/", mealServer.handleHTTP)

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	mealServer.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return mealServer, nil
}

func (s *MealLogServer) handleHTTP(w http.ResponseWriter, r *http.Request) {
	// Simple HTTP-based MCP protocol handler
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == http.MethodOptions {
		return
	}

	// Decode the MCP request
	var request protocol.CallToolRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Route to appropriate handler based on tool name
	var result *protocol.CallToolResult
	var err error

	switch request.Name {
	case "log_meal":
		result, err = s.handleLogMeal(&request)
	case "calculate_carbs":
		result, err = s.handleCalculateCarbs(&request)
	case "get_meals":
		result, err = s.handleGetMeals(&request)
	default:
		http.Error(w, fmt.Sprintf("Unknown tool: %s", request.Name), http.StatusNotFound)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Send response
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Printf("Failed to encode response: %v", err)
	}
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

func (s *MealLogServer) createJSONResponse(data interface{}) (*protocol.CallToolResult, error) {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	// Fixed: Use proper protocol.Content types
	return &protocol.CallToolResult{
		Content: []protocol.Content{
			protocol.TextContent{
				Type: "text",
				Text: string(jsonBytes),
			},
		},
	}, nil
}
