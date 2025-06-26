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

// MCPRequest represents a simplified MCP tool call request
type MCPRequest struct {
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params"`
}

// MCPResponse represents a simplified MCP response
type MCPResponse struct {
	Result interface{} `json:"result,omitempty"`
	Error  *MCPError   `json:"error,omitempty"`
}

type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
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
	mux.HandleFunc("/", mealServer.handleHTTP)
	mux.HandleFunc("/log_meal", mealServer.handleLogMealHTTP)
	mux.HandleFunc("/calculate_carbs", mealServer.handleCalculateCarbsHTTP)
	mux.HandleFunc("/get_meals", mealServer.handleGetMealsHTTP)

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	mealServer.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	log.Printf("Meal log server configured on %s", addr)
	return mealServer, nil
}

func (s *MealLogServer) handleHTTP(w http.ResponseWriter, r *http.Request) {
	// Handle CORS
	s.setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		return
	}

	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Decode the request
	var request MCPRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		s.sendError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Route to appropriate handler
	var result interface{}
	var err error

	switch request.Method {
	case "log_meal":
		result, err = s.logMeal(request.Params)
	case "calculate_carbs":
		result, err = s.calculateCarbs(request.Params)
	case "get_meals":
		result, err = s.getMeals(request.Params)
	default:
		s.sendError(w, http.StatusNotFound, fmt.Sprintf("Unknown method: %s", request.Method))
		return
	}

	if err != nil {
		s.sendError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Send success response
	response := MCPResponse{Result: result}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *MealLogServer) handleLogMealHTTP(w http.ResponseWriter, r *http.Request) {
	s.setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		return
	}

	var params map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		s.sendError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := s.logMeal(params)
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *MealLogServer) handleCalculateCarbsHTTP(w http.ResponseWriter, r *http.Request) {
	s.setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		return
	}

	var params map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		s.sendError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := s.calculateCarbs(params)
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *MealLogServer) handleGetMealsHTTP(w http.ResponseWriter, r *http.Request) {
	s.setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		return
	}

	var params map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		s.sendError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := s.getMeals(params)
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *MealLogServer) setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

func (s *MealLogServer) sendError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	response := MCPResponse{
		Error: &MCPError{
			Code:    statusCode,
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
