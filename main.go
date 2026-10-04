package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
)

// =====================================
// KONFIGURASI
// =====================================

const (
	ESP8266URL = "http://103.160.63.215"

	// Gunakan model Gemini yang tersedia di API key/project kamu.
	GEMINIURL = "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.8-flash:generateContent"
)

// =====================================
// DATA DARI ESP8266
// =====================================

type ESPResponse struct {
	Success bool       `json:"success"`
	Message string     `json:"message,omitempty"`
	Data    SensorData `json:"data"`
}

type SensorData struct {
	Suhu       float64 `json:"suhu"`
	Kelembapan float64 `json:"kelembapan"`
	Status     string  `json:"status"`
}

// =====================================
// HASIL ANALISIS AI
// =====================================

type AIAnalysis struct {
	Kondisi  string `json:"kondisi"`
	Bahaya   bool   `json:"bahaya"`
	Analisis string `json:"analisis"`
	Saran    string `json:"saran"`
}

// =====================================
// RESPONSE GEMINI
// =====================================

type GeminiRequest struct {
	Contents []GeminiContent `json:"contents"`
}

type GeminiContent struct {
	Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
	Text string `json:"text"`
}

type GeminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// =====================================
// RESPONSE API
// =====================================

type SensorAPIResponse struct {
	Success    bool       `json:"success"`
	Data       SensorData `json:"data"`
	AnalisisAI AIAnalysis `json:"analisis_ai"`
}

// =====================================
// MAIN
// =====================================

func main() {

	// Load file .env
	err := godotenv.Load()

	if err != nil {
		fmt.Println("Warning: .env file not found")
	}

	// Endpoint sensor
	http.HandleFunc("/api/sensor", sensorHandler)

	// Health check
	http.HandleFunc("/health", healthHandler)

	fmt.Println("================================")
	fmt.Println(" IoT Peternakan API")
	fmt.Println("================================")
	fmt.Println("Server running on :8080")
	fmt.Println("ESP8266:", ESP8266URL)
	fmt.Println("================================")

	log.Fatal(
		http.ListenAndServe(":8080", nil),
	)
}

// =====================================
// HEALTH CHECK
// =====================================

func healthHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	if r.Method != http.MethodGet {

		writeJSON(
			w,
			http.StatusMethodNotAllowed,
			map[string]interface{}{
				"success": false,
				"message": "method not allowed, use GET",
			},
		)

		return
	}

	writeJSON(
		w,
		http.StatusOK,
		map[string]interface{}{
			"success": true,
			"message": "API is running",
		},
	)
}

// =====================================
// GET /api/sensor
// =====================================

func sensorHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	if r.Method != http.MethodGet {

		writeJSON(
			w,
			http.StatusMethodNotAllowed,
			map[string]interface{}{
				"success": false,
				"message": "method not allowed, use GET",
			},
		)

		return
	}

	// =================================
	// 1. AMBIL DATA DARI ESP8266
	// =================================

	sensorData, err := getSensorFromESP8266()

	if err != nil {

		log.Println(
			"Gagal mengambil data ESP8266:",
			err,
		)

		writeJSON(
			w,
			http.StatusBadGateway,
			map[string]interface{}{
				"success": false,
				"message": "gagal menghubungi ESP8266",
				"error":   err.Error(),
			},
		)

		return
	}

	fmt.Println()
	fmt.Println("================================")
	fmt.Println(" DATA SENSOR")
	fmt.Println("================================")
	fmt.Printf(
		"Suhu       : %.1f C\n",
		sensorData.Suhu,
	)
	fmt.Printf(
		"Kelembapan : %.1f %%\n",
		sensorData.Kelembapan,
	)
	fmt.Printf(
		"Status     : %s\n",
		sensorData.Status,
	)

	// =================================
	// 2. ANALISIS AI
	// =================================

	aiResult, err := analyzeWithGemini(
		sensorData,
	)

	if err != nil {

		log.Println(
			"Gagal melakukan analisis AI:",
			err,
		)

		writeJSON(
			w,
			http.StatusBadGateway,
			map[string]interface{}{
				"success": false,
				"message": "data sensor berhasil diambil tetapi analisis AI gagal",
				"data":    sensorData,
				"error":   err.Error(),
			},
		)

		return
	}

	// =================================
	// 3. GABUNGKAN RESPONSE
	// =================================

	response := SensorAPIResponse{
		Success:    true,
		Data:       sensorData,
		AnalisisAI: aiResult,
	}

	writeJSON(
		w,
		http.StatusOK,
		response,
	)
}

// =====================================
// AMBIL DATA DARI ESP8266
// =====================================

func getSensorFromESP8266() (
	SensorData,
	error,
) {

	var data SensorData

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Request:
	// GET http://192.168.68.105/sensor

	resp, err := client.Get(
		ESP8266URL + "/sensor",
	)

	if err != nil {
		return data, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {

		return data, fmt.Errorf(
			"ESP8266 mengembalikan HTTP status %d",
			resp.StatusCode,
		)
	}

	var espResponse ESPResponse

	err = json.NewDecoder(
		resp.Body,
	).Decode(&espResponse)

	if err != nil {
		return data, err
	}

	if !espResponse.Success {

		return data, fmt.Errorf(
			"ESP8266 error: %s",
			espResponse.Message,
		)
	}

	return espResponse.Data, nil
}

// =====================================
// ANALISIS GEMINI
// =====================================

func analyzeWithGemini(
	sensor SensorData,
) (AIAnalysis, error) {

	var result AIAnalysis

	apiKey := os.Getenv(
		"GEMINI_API_KEY",
	)

	if apiKey == "" {

		return result, fmt.Errorf(
			"GEMINI_API_KEY tidak ditemukan",
		)
	}

	// =================================
	// PROMPT
	// =================================

	prompt := fmt.Sprintf(`
Kamu adalah sistem analisis kondisi lingkungan
untuk peternakan ayam kampung.

Berikut data sensor kandang:

Suhu: %.1f °C
Kelembapan: %.1f %%
Status sensor: %s

Analisis kondisi lingkungan tersebut untuk ayam kampung.

Pertimbangkan:
- Apakah suhu terlalu rendah, sesuai, atau terlalu tinggi.
- Apakah kelembapan terlalu rendah, sesuai, atau terlalu tinggi.
- Risiko lingkungan terhadap kenyamanan ayam.
- Apakah kondisi perlu diperhatikan oleh peternak.
- Berikan saran praktis seperti ventilasi, sirkulasi udara,
  perlindungan dari dingin, atau pengendalian kelembapan.

Jangan mendiagnosis penyakit ayam.
Analisis hanya kondisi lingkungan berdasarkan suhu
dan kelembapan.

Gunakan kategori:
AMAN
WASPADA
BAHAYA

Kembalikan HANYA JSON valid dengan format:

{
  "kondisi": "AMAN",
  "bahaya": false,
  "analisis": "penjelasan kondisi",
  "saran": "saran untuk peternak"
}

Data sensor:
Suhu = %.1f °C
Kelembapan = %.1f %%
`,
		sensor.Suhu,
		sensor.Kelembapan,
		sensor.Status,
		sensor.Suhu,
		sensor.Kelembapan,
	)

	// =================================
	// REQUEST GEMINI
	// =================================

	geminiRequest := GeminiRequest{
		Contents: []GeminiContent{
			{
				Parts: []GeminiPart{
					{
						Text: prompt,
					},
				},
			},
		},
	}

	jsonBody, err := json.Marshal(
		geminiRequest,
	)

	if err != nil {
		return result, err
	}

	// =================================
	// HTTP REQUEST
	// =================================

	req, err := http.NewRequest(
		http.MethodPost,
		GEMINIURL,
		bytes.NewBuffer(jsonBody),
	)

	if err != nil {
		return result, err
	}

	req.Header.Set(
		"Content-Type",
		"application/json",
	)

	req.Header.Set(
		"x-goog-api-key",
		apiKey,
	)

	client := &http.Client{
		Timeout: 20 * time.Second,
	}

	resp, err := client.Do(req)

	if err != nil {
		return result, err
	}

	defer resp.Body.Close()

	// =================================
	// BACA RESPONSE
	// =================================

	responseBody, err := io.ReadAll(
		resp.Body,
	)

	if err != nil {
		return result, err
	}

	if resp.StatusCode != http.StatusOK {

		return result, fmt.Errorf(
			"Gemini HTTP status %d: %s",
			resp.StatusCode,
			string(responseBody),
		)
	}

	var geminiResponse GeminiResponse

	err = json.Unmarshal(
		responseBody,
		&geminiResponse,
	)

	if err != nil {
		return result, err
	}

	// =================================
	// VALIDASI RESPONSE
	// =================================

	if len(geminiResponse.Candidates) == 0 {

		return result, fmt.Errorf(
			"Gemini tidak memberikan response",
		)
	}

	if len(
		geminiResponse.
			Candidates[0].
			Content.
			Parts,
	) == 0 {

		return result, fmt.Errorf(
			"Gemini response kosong",
		)
	}

	answer :=
		geminiResponse.
			Candidates[0].
			Content.
			Parts[0].
			Text

	fmt.Println()
	fmt.Println("================================")
	fmt.Println(" ANALISIS AI")
	fmt.Println("================================")
	fmt.Println(answer)

	// =================================
	// PARSE JSON GEMINI
	// =================================

	err = json.Unmarshal(
		[]byte(answer),
		&result,
	)

	if err != nil {

		return result, fmt.Errorf(
			"response Gemini bukan JSON valid: %w",
			err,
		)
	}

	return result, nil
}

// =====================================
// WRITE JSON
// =====================================

func writeJSON(
	w http.ResponseWriter,
	statusCode int,
	data interface{},
) {

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(statusCode)
	err := json.NewEncoder(w).Encode(
		data,
	)

	if err != nil {

		log.Println(
			"Gagal mengirim response JSON:",
			err,
		)
	}
}
