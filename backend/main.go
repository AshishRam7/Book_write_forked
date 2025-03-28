package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/golang-jwt/jwt/v5" // Import the JWT library
	"github.com/joho/godotenv"
)

// ****** ADD THESE STRUCT DEFINITIONS BACK *****

// BookRequest represents the request body for book generation.
type BookRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Chapters    int    `json:"chapters"`
	// ApiKey is removed as we rely on JWT auth now
}

// QwenMessage represents a message in the Qwen API request.
type QwenMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// QwenAPIRequest represents the request body for the Qwen API.
type QwenAPIRequest struct {
	Model        string `json:"model"`
	Input        struct {
		Messages []QwenMessage `json:"messages"`
	} `json:"input"`
	ResultFormat string `json:"result_format"`
}

// QwenResponse represents the response from the Qwen API.
type QwenResponse struct {
	Output struct {
		FinishReason string `json:"finish_reason"`
		Text         string `json:"text"`
	} `json:"output"`
}

// ****** END OF ADDED STRUCT DEFINITIONS ******


// --- JWT Middleware ---
func jwtMiddleware(c fiber.Ctx) error {
	authHeader := c.Get("Authorization")
	if authHeader == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Missing or malformed JWT"})
	}

	// Check if the header format is "Bearer token"
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Malformed Authorization header"})
	}
	tokenString := parts[1]

	// Get the secret key from environment variable
	jwtSecret := os.Getenv("POCKETBASE_TOKEN_SECRET")
	if jwtSecret == "" {
		log.Println("Error: POCKETBASE_TOKEN_SECRET environment variable not set")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Server configuration error"})
	}

	// Parse and validate the token
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Don't forget to validate the alg is what you expect:
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(jwtSecret), nil
	})

	if err != nil {
		log.Printf("JWT validation error: %v\n", err)
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid or expired JWT"})
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		// Token is valid. You can optionally extract user info from claims
		// and add it to the context if needed by downstream handlers.
		// For example: c.Locals("userID", claims["id"])
		// Check token type if necessary (e.g., ensure it's an 'auth' token)
		tokenType, ok := claims["type"].(string) // Add type assertion check
		if !ok || tokenType != "auth" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid token type"})
		}

		log.Printf("JWT valid for user ID: %v\n", claims["id"]) // Example claim access
		return c.Next()
	}

	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid JWT"})
}

func main() {
	// Load environment variables from .env file.
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: Error loading .env file:", err)
	}

	app := fiber.New()

	// --- FIX CORS ---
	// Use comma-separated strings first, ensuring Authorization is included.
	// If you still get the "cannot use...as []string" error, change to the commented-out slice version.
	app.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"}, // Use slice
		AllowHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization"}, // Use slice of strings
	}))

	app.Get("/", func(c fiber.Ctx) error {
		return c.SendString("Hello There!")
	})

	app.Get("/health", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status":    "healthy",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	})

	// Apply JWT middleware ONLY to the protected route
	app.Post("/generate-book", jwtMiddleware, generateBook)

	// Get port from environment variable or use default.
	port := os.Getenv("PORT")
	if port == "" {
		port = "5000" // Default to 5000 to avoid conflict with PocketBase/Astro dev
	}

	fmt.Printf("Go backend listening on port %s\n", port)
	log.Fatal(app.Listen(":" + port))
}

// --- generateBook function ---
func generateBook(c fiber.Ctx) error {
	// Parse the request body using Bind().Body().
	var req BookRequest // Now defined
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body: " + err.Error(),
		})
	}

	// Validate the request.
	if req.Title == "" || req.Description == "" || req.Chapters <= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Title, description, and chapters are required",
		})
	}


	// Get the Qwen API key from the environment variable.
	apiKey := os.Getenv("QWEN_API_KEY")
	if apiKey == "" {
		log.Println("Error: QWEN_API_KEY environment variable not set")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Server configuration error (missing AI API key)",
		})
	}

	// Create the system prompt.
	systemPrompt := fmt.Sprintf(`You are a professional book writer.
Generate a complete book with the following details:
- Title: %s
- Description: %s
- Number of chapters: %d

The book should have a coherent narrative that follows the description.
Each chapter should have a title and substantial content.
Format the book with proper Markdown, including headings for chapters.
Create a compelling opening and satisfying conclusion.`, req.Title, req.Description, req.Chapters)

	// Create the Qwen API request payload.
	var qwenReq QwenAPIRequest // Now defined
	qwenReq.Model = "qwen-plus"
	qwenReq.ResultFormat = "message"
	qwenReq.Input.Messages = []QwenMessage{ // Now defined
		{
			Role:    "system",
			Content: systemPrompt,
		},
		{
			Role:    "user",
			Content: fmt.Sprintf("Please generate a complete book titled '%s' with %d chapters based on this description: %s", req.Title, req.Chapters, req.Description),
		},
	}

	// Call the Qwen API.
	bookContent, err := callQwenAPI(qwenReq, apiKey)
	if err != nil {
		// Log the specific error from callQwenAPI
		log.Printf("Error calling Qwen API: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to generate book: " + err.Error(),
		})
	}

	// Return the generated book.
	return c.JSON(fiber.Map{
		"book": bookContent,
	})
}

// --- callQwenAPI function ---
func callQwenAPI(req QwenAPIRequest, apiKey string) (string, error) { // Use defined type
	// Marshal the request payload into JSON.
	jsonData, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create the HTTP request with the proper Qwen API endpoint.
	url := "https://dashscope-intl.aliyuncs.com/api/v1/services/aigc/text-generation/generation"
	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Create a context with a 6-minute timeout and attach it to the request.
	ctx, cancel := context.WithTimeout(context.Background(), 360*time.Second)
	defer cancel()
	httpReq = httpReq.WithContext(ctx)

	// Set the required headers.
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey) // Qwen API Key

	// Increased client timeout to match context
	client := &http.Client{Timeout: 360 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		// Check for context deadline exceeded
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("request timed out after 6 minutes: %w", err)
		}
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read the response.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Qwen API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the JSON response.
	var qwenResp QwenResponse // Now defined
	if err := json.Unmarshal(body, &qwenResp); err != nil {
		return "", fmt.Errorf("failed to parse Qwen JSON response: %w, response body: %s", err, string(body))
	}

	if qwenResp.Output.Text == "" {
		// Log the full response for debugging empty text issues
		log.Printf("Warning: Qwen API returned empty text. FinishReason: '%s'. Full response: %+v\n", qwenResp.Output.FinishReason, qwenResp)
		finishReason := qwenResp.Output.FinishReason
		if finishReason != "stop" && finishReason != "" {
			return "", fmt.Errorf("API generation finished unexpectedly with reason: %s", finishReason)
		}
		// Consider if empty text is possible valid output or always an error
		return "", fmt.Errorf("API returned an empty response text") // Treat as error for now
	}

	return qwenResp.Output.Text, nil
}