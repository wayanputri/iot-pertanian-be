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
	"time"

	"github.com/joho/godotenv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
)

// =====================================
// KONFIGURASI
// =====================================

const (
	ESP8266URL = "http://103.160.63.215:8081"

	// Gunakan model Gemini yang tersedia
	// di API key/project kamu.
	GEMINIURL = "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.8-flash:generateContent"

	SERVICE_NAME = "iot-peternakan-api"
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
// REQUEST GEMINI
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

// =====================================
// RESPONSE GEMINI
// =====================================

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
// INIT JAEGER / OPENTELEMETRY
// =====================================

func initTracer(ctx context.Context) (*sdktrace.TracerProvider, error) {

	/*
		JAEGER_ENDPOINT di Docker:

		JAEGER_ENDPOINT=jaeger:4318

		Karena OTLP HTTP exporter membutuhkan
		host:port, bukan http://host:port.
	*/

	endpoint := os.Getenv("JAEGER_ENDPOINT")

	if endpoint == "" {
		endpoint = "localhost:4318"
	}

	log.Println("Jaeger endpoint:", endpoint)

	exporter, err := otlptracehttp.New(
		ctx,
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
	)

	if err != nil {
		return nil, fmt.Errorf(
			"gagal membuat Jaeger exporter: %w",
			err,
		)
	}

	res, err := resource.New(
		ctx,

		resource.WithAttributes(
			semconv.ServiceName(SERVICE_NAME),
			attribute.String(
				"service.version",
				"1.0.0",
			),
		),
	)

	if err != nil {
		return nil, fmt.Errorf(
			"gagal membuat resource tracing: %w",
			err,
		)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)

	return tp, nil
}

// =====================================
// MAIN
// =====================================

func main() {

	// =================================
	// LOAD ENV
	// =================================

	err := godotenv.Load()

	if err != nil {
		fmt.Println("Warning: .env file not found")
	}

	// =================================
	// INIT TRACER
	// =================================

	ctx := context.Background()

	tp, err := initTracer(ctx)

	if err != nil {
		log.Fatal(err)
	}

	defer func() {

		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)

		defer cancel()

		if err := tp.Shutdown(shutdownCtx); err != nil {
			log.Println(
				"gagal shutdown tracer:",
				err,
			)
		}

	}()

	// =================================
	// ENDPOINT
	// =================================

	http.HandleFunc(
		"/api/sensor",
		sensorHandler,
	)

	http.HandleFunc(
		"/health",
		healthHandler,
	)

	// =================================
	// START SERVER
	// =================================

	fmt.Println("================================")
	fmt.Println(" IoT Peternakan API")
	fmt.Println("================================")
	fmt.Println("Server running on :8080")
	fmt.Println("ESP8266:", ESP8266URL)
	fmt.Println("Jaeger:", os.Getenv("JAEGER_ENDPOINT"))
	fmt.Println("================================")

	log.Fatal(
		http.ListenAndServe(
			":8080",
			nil,
		),
	)
}

// =====================================
// HEALTH CHECK
// =====================================

func healthHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	tracer := otel.Tracer(SERVICE_NAME)

	ctx, span := tracer.Start(
		r.Context(),
		"GET /health",
	)

	defer span.End()

	start := time.Now()

	log.Println("================================")
	log.Println("[REQUEST]")
	log.Println("Method:", r.Method)
	log.Println("Path:", r.URL.Path)

	if r.Method != http.MethodGet {

		span.SetAttributes(
			attribute.Int(
				"http.response.status_code",
				http.StatusMethodNotAllowed,
			),
		)

		writeJSON(
			w,
			http.StatusMethodNotAllowed,
			map[string]interface{}{
				"success": false,
				"message": "method not allowed, use GET",
			},
		)

		log.Println(
			"[RESPONSE]",
			http.StatusMethodNotAllowed,
			"duration:",
			time.Since(start),
		)

		return
	}

	response := map[string]interface{}{
		"success": true,
		"message": "API is running",
	}

	writeJSON(
		w,
		http.StatusOK,
		response,
	)

	span.SetAttributes(
		attribute.Int(
			"http.response.status_code",
			http.StatusOK,
		),
	)

	// Hindari ctx tidak digunakan setelah span dibuat.
	_ = ctx

	log.Println(
		"[RESPONSE]",
		http.StatusOK,
		"duration:",
		time.Since(start),
	)

	log.Println("================================")
}

// =====================================
// GET /api/sensor
// =====================================

func sensorHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	tracer := otel.Tracer(SERVICE_NAME)

	ctx, span := tracer.Start(
		r.Context(),
		"GET /api/sensor",
	)

	defer span.End()

	start := time.Now()

	log.Println()
	log.Println("================================")
	log.Println("[REQUEST]")
	log.Println("Method:", r.Method)
	log.Println("Path:", r.URL.Path)
	log.Println("Time:", start.Format(time.RFC3339))

	span.SetAttributes(
		attribute.String(
			"http.request.method",
			r.Method,
		),
		attribute.String(
			"url.path",
			r.URL.Path,
		),
	)

	// =================================
	// VALIDASI METHOD
	// =================================

	if r.Method != http.MethodGet {

		span.SetAttributes(
			attribute.Int(
				"http.response.status_code",
				http.StatusMethodNotAllowed,
			),
		)

		writeJSON(
			w,
			http.StatusMethodNotAllowed,
			map[string]interface{}{
				"success": false,
				"message": "method not allowed, use GET",
			},
		)

		log.Println(
			"[RESPONSE]",
			http.StatusMethodNotAllowed,
			"duration:",
			time.Since(start),
		)

		return
	}

	// =================================
	// 1. AMBIL DATA ESP8266
	// =================================

	log.Println("[STEP 1] Mengambil data ESP8266...")

	sensorData, err := getSensorFromESP8266(ctx)

	if err != nil {

		log.Println(
			"[ERROR] Gagal mengambil data ESP8266:",
			err,
		)

		span.RecordError(err)
		span.SetStatus(
			codes.Error,
			"gagal mengambil data ESP8266",
		)

		span.SetAttributes(
			attribute.Int(
				"http.response.status_code",
				http.StatusBadGateway,
			),
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

		log.Println(
			"[RESPONSE]",
			http.StatusBadGateway,
			"duration:",
			time.Since(start),
		)

		log.Println("================================")

		return
	}

	// =================================
	// DATA SENSOR
	// =================================

	log.Println()
	log.Println("================================")
	log.Println(" DATA SENSOR")
	log.Println("================================")

	log.Printf(
		"Suhu       : %.1f C\n",
		sensorData.Suhu,
	)

	log.Printf(
		"Kelembapan : %.1f %%\n",
		sensorData.Kelembapan,
	)

	log.Printf(
		"Status     : %s\n",
		sensorData.Status,
	)

	span.SetAttributes(
		attribute.Float64(
			"sensor.suhu",
			sensorData.Suhu,
		),
		attribute.Float64(
			"sensor.kelembapan",
			sensorData.Kelembapan,
		),
		attribute.String(
			"sensor.status",
			sensorData.Status,
		),
	)

	// =================================
	// 2. ANALISIS AI
	// =================================

	log.Println("[STEP 2] Mengirim data ke Gemini...")

	aiResult, err := analyzeWithGemini(
		ctx,
		sensorData,
	)

	if err != nil {

		log.Println(
			"[ERROR] Gagal melakukan analisis AI:",
			err,
		)

		span.RecordError(err)
		span.SetStatus(
			codes.Error,
			"analisis AI gagal",
		)

		span.SetAttributes(
			attribute.Int(
				"http.response.status_code",
				http.StatusBadGateway,
			),
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

		log.Println(
			"[RESPONSE]",
			http.StatusBadGateway,
			"duration:",
			time.Since(start),
		)

		log.Println("================================")

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

	// =================================
	// RESPONSE API
	// =================================

	writeJSON(
		w,
		http.StatusOK,
		response,
	)

	span.SetAttributes(
		attribute.Int(
			"http.response.status_code",
			http.StatusOK,
		),
	)

	span.SetStatus(
		codes.Ok,
		"request berhasil",
	)

	log.Println()
	log.Println("[RESPONSE]")
	log.Println("Status:", http.StatusOK)
	log.Println(
		"Duration:",
		time.Since(start),
	)

	log.Println("================================")
}

// =====================================
// AMBIL DATA DARI ESP8266
// =====================================

func getSensorFromESP8266(
	ctx context.Context,
) (
	SensorData,
	error,
) {

	var data SensorData

	tracer := otel.Tracer(SERVICE_NAME)

	ctx, span := tracer.Start(
		ctx,
		"GET ESP8266 /sensor",
	)

	defer span.End()

	// =================================
	// HTTP CLIENT
	// =================================

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	url := ESP8266URL + "/sensor"

	// =================================
	// CREATE REQUEST
	// =================================

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		url,
		nil,
	)

	if err != nil {

		span.RecordError(err)
		span.SetStatus(
			codes.Error,
			"gagal membuat request ESP8266",
		)

		return data, err
	}

	span.SetAttributes(
		attribute.String(
			"http.request.method",
			http.MethodGet,
		),
		attribute.String(
			"server.address",
			ESP8266URL,
		),
	)

	// =================================
	// REQUEST LOG
	// =================================

	start := time.Now()

	log.Println()
	log.Println("--------------------------------")
	log.Println("[ESP8266 REQUEST]")
	log.Println("Method:", http.MethodGet)
	log.Println("URL:", url)
	log.Println("--------------------------------")

	// =================================
	// SEND REQUEST
	// =================================

	resp, err := client.Do(req)

	duration := time.Since(start)

	if err != nil {

		log.Println("[ESP8266 ERROR]")
		log.Println("Error:", err)
		log.Println("Duration:", duration)
		log.Println("--------------------------------")

		span.RecordError(err)

		span.SetStatus(
			codes.Error,
			"ESP8266 request gagal",
		)

		return data, err
	}

	defer resp.Body.Close()

	// =================================
	// RESPONSE LOG
	// =================================

	log.Println("[ESP8266 RESPONSE]")
	log.Println("Status:", resp.StatusCode)
	log.Println("Duration:", duration)

	span.SetAttributes(
		attribute.Int(
			"http.response.status_code",
			resp.StatusCode,
		),
		attribute.Int64(
			"http.response.duration_ms",
			duration.Milliseconds(),
		),
	)

	// =================================
	// STATUS CHECK
	// =================================

	if resp.StatusCode != http.StatusOK {

		err := fmt.Errorf(
			"ESP8266 mengembalikan HTTP status %d",
			resp.StatusCode,
		)

		span.RecordError(err)

		span.SetStatus(
			codes.Error,
			"ESP8266 HTTP error",
		)

		return data, err
	}

	// =================================
	// READ RESPONSE
	// =================================

	responseBody, err := io.ReadAll(
		resp.Body,
	)

	if err != nil {

		span.RecordError(err)
		span.SetStatus(
			codes.Error,
			"gagal membaca response ESP8266",
		)

		return data, err
	}

	log.Println(
		"[ESP8266 RESPONSE BODY]",
		string(responseBody),
	)

	// =================================
	// PARSE RESPONSE
	// =================================

	var espResponse ESPResponse

	err = json.Unmarshal(
		responseBody,
		&espResponse,
	)

	if err != nil {

		span.RecordError(err)
		span.SetStatus(
			codes.Error,
			"response ESP8266 bukan JSON valid",
		)

		return data, err
	}

	// =================================
	// VALIDASI RESPONSE
	// =================================

	if !espResponse.Success {

		err := fmt.Errorf(
			"ESP8266 error: %s",
			espResponse.Message,
		)

		span.RecordError(err)

		span.SetStatus(
			codes.Error,
			"ESP8266 mengembalikan error",
		)

		return data, err
	}

	// =================================
	// SENSOR DATA
	// =================================

	log.Println("[ESP8266 DATA]")
	log.Printf(
		"Suhu: %.1f C\n",
		espResponse.Data.Suhu,
	)

	log.Printf(
		"Kelembapan: %.1f %%\n",
		espResponse.Data.Kelembapan,
	)

	log.Printf(
		"Status: %s\n",
		espResponse.Data.Status,
	)

	span.SetAttributes(
		attribute.Float64(
			"sensor.suhu",
			espResponse.Data.Suhu,
		),
		attribute.Float64(
			"sensor.kelembapan",
			espResponse.Data.Kelembapan,
		),
		attribute.String(
			"sensor.status",
			espResponse.Data.Status,
		),
	)

	span.SetStatus(
		codes.Ok,
		"ESP8266 request berhasil",
	)

	return espResponse.Data, nil
}

// =====================================
// ANALISIS GEMINI
// =====================================

func analyzeWithGemini(
	ctx context.Context,
	sensor SensorData,
) (
	AIAnalysis,
	error,
) {

	var result AIAnalysis

	tracer := otel.Tracer(SERVICE_NAME)

	ctx, span := tracer.Start(
		ctx,
		"POST Gemini",
	)

	defer span.End()

	// =================================
	// API KEY
	// =================================

	apiKey := os.Getenv(
		"GEMINI_API_KEY",
	)

	if apiKey == "" {

		err := fmt.Errorf(
			"GEMINI_API_KEY tidak ditemukan",
		)

		span.RecordError(err)

		span.SetStatus(
			codes.Error,
			"Gemini API key tidak ditemukan",
		)

		return result, err
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

		span.RecordError(err)
		span.SetStatus(
			codes.Error,
			"gagal marshal request Gemini",
		)

		return result, err
	}

	// =================================
	// HTTP REQUEST
	// =================================

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		GEMINIURL,
		bytes.NewBuffer(jsonBody),
	)

	if err != nil {

		span.RecordError(err)

		span.SetStatus(
			codes.Error,
			"gagal membuat request Gemini",
		)

		return result, err
	}

	req.Header.Set(
		"Content-Type",
		"application/json",
	)

	// IMPORTANT:
	// API KEY sengaja tidak dimasukkan
	// ke log/tracing.

	req.Header.Set(
		"x-goog-api-key",
		apiKey,
	)

	span.SetAttributes(
		attribute.String(
			"http.request.method",
			http.MethodPost,
		),
		attribute.String(
			"server.address",
			"generativelanguage.googleapis.com",
		),
	)

	client := &http.Client{
		Timeout: 20 * time.Second,
	}

	// =================================
	// REQUEST LOG
	// =================================

	start := time.Now()

	log.Println()
	log.Println("--------------------------------")
	log.Println("[GEMINI REQUEST]")
	log.Println("Method:", http.MethodPost)
	log.Println("URL:", GEMINIURL)
	log.Println("Content-Type: application/json")
	log.Println("--------------------------------")

	// =================================
	// SEND REQUEST
	// =================================

	resp, err := client.Do(req)

	duration := time.Since(start)

	if err != nil {

		log.Println("[GEMINI ERROR]")
		log.Println("Error:", err)
		log.Println("Duration:", duration)
		log.Println("--------------------------------")

		span.RecordError(err)

		span.SetStatus(
			codes.Error,
			"Gemini request gagal",
		)

		return result, err
	}

	defer resp.Body.Close()

	// =================================
	// RESPONSE STATUS
	// =================================

	log.Println("[GEMINI RESPONSE]")
	log.Println("Status:", resp.StatusCode)
	log.Println("Duration:", duration)

	span.SetAttributes(
		attribute.Int(
			"http.response.status_code",
			resp.StatusCode,
		),
		attribute.Int64(
			"http.response.duration_ms",
			duration.Milliseconds(),
		),
	)

	// =================================
	// READ RESPONSE
	// =================================

	responseBody, err := io.ReadAll(
		resp.Body,
	)

	if err != nil {

		span.RecordError(err)

		span.SetStatus(
			codes.Error,
			"gagal membaca response Gemini",
		)

		return result, err
	}

	// Jangan log API key.
	// Response Gemini aman untuk debugging,
	// tapi sebaiknya jangan digunakan untuk
	// production logging jika berisi data sensitif.

	log.Println(
		"[GEMINI RESPONSE BODY]",
		string(responseBody),
	)

	// =================================
	// HTTP STATUS CHECK
	// =================================

	if resp.StatusCode != http.StatusOK {

		err := fmt.Errorf(
			"Gemini HTTP status %d: %s",
			resp.StatusCode,
			string(responseBody),
		)

		span.RecordError(err)

		span.SetStatus(
			codes.Error,
			"Gemini HTTP error",
		)

		return result, err
	}

	// =================================
	// PARSE RESPONSE
	// =================================

	var geminiResponse GeminiResponse

	err = json.Unmarshal(
		responseBody,
		&geminiResponse,
	)

	if err != nil {

		span.RecordError(err)

		span.SetStatus(
			codes.Error,
			"response Gemini bukan JSON valid",
		)

		return result, err
	}

	// =================================
	// VALIDASI CANDIDATE
	// =================================

	if len(geminiResponse.Candidates) == 0 {

		err := fmt.Errorf(
			"Gemini tidak memberikan response",
		)

		span.RecordError(err)

		span.SetStatus(
			codes.Error,
			"Gemini tidak memberikan candidate",
		)

		return result, err
	}

	// =================================
	// VALIDASI PART
	// =================================

	if len(
		geminiResponse.
			Candidates[0].
			Content.
			Parts,
	) == 0 {

		err := fmt.Errorf(
			"Gemini response kosong",
		)

		span.RecordError(err)

		span.SetStatus(
			codes.Error,
			"Gemini response kosong",
		)

		return result, err
	}

	// =================================
	// AMBIL ANSWER
	// =================================

	answer :=
		geminiResponse.
			Candidates[0].
			Content.
			Parts[0].
			Text

	log.Println()
	log.Println("================================")
	log.Println(" ANALISIS AI")
	log.Println("================================")
	log.Println(answer)

	// =================================
	// PARSE JSON GEMINI
	// =================================

	err = json.Unmarshal(
		[]byte(answer),
		&result,
	)

	if err != nil {

		err := fmt.Errorf(
			"response Gemini bukan JSON valid: %w",
			err,
		)

		span.RecordError(err)

		span.SetStatus(
			codes.Error,
			"Gemini response JSON invalid",
		)

		return result, err
	}

	// =================================
	// TRACING RESULT
	// =================================

	span.SetAttributes(
		attribute.String(
			"ai.kondisi",
			result.Kondisi,
		),
		attribute.Bool(
			"ai.bahaya",
			result.Bahaya,
		),
	)

	span.SetStatus(
		codes.Ok,
		"Gemini request berhasil",
	)

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
