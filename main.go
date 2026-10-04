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
	"go.opentelemetry.io/otel/trace"
)

// =====================================
// KONFIGURASI
// =====================================

const (
	ESP8266URL = "http://192.168.68.103"

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
		log.Println("Warning: .env file not found")
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

	log.Println("================================")
	log.Println(" IoT Peternakan API")
	log.Println("================================")
	log.Println("Server running on :8080")
	log.Println("ESP8266:", ESP8266URL)
	log.Println("Jaeger:", os.Getenv("JAEGER_ENDPOINT"))
	log.Println("================================")

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

	// =================================
	// REQUEST TRACE
	// =================================

	span.AddEvent(
		"HTTP REQUEST",
		trace.WithAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.path", r.URL.Path),
		),
	)

	span.SetAttributes(
		attribute.String("http.request.method", r.Method),
		attribute.String("url.path", r.URL.Path),
	)

	if r.Method != http.MethodGet {

		span.SetAttributes(
			attribute.Int(
				"http.response.status_code",
				http.StatusMethodNotAllowed,
			),
		)

		span.AddEvent(
			"HTTP RESPONSE",
			trace.WithAttributes(
				attribute.Int(
					"http.status_code",
					http.StatusMethodNotAllowed,
				),
				attribute.Int64(
					"duration_ms",
					time.Since(start).Milliseconds(),
				),
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

	span.SetStatus(
		codes.Ok,
		"health check berhasil",
	)

	span.AddEvent(
		"HTTP RESPONSE",
		trace.WithAttributes(
			attribute.Int(
				"http.status_code",
				http.StatusOK,
			),
			attribute.Int64(
				"duration_ms",
				time.Since(start).Milliseconds(),
			),
		),
	)

	_ = ctx
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

	// =================================
	// REQUEST TRACE
	// =================================

	span.AddEvent(
		"HTTP REQUEST",
		trace.WithAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.path", r.URL.Path),
		),
	)

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

		span.SetStatus(
			codes.Error,
			"method not allowed",
		)

		span.AddEvent(
			"HTTP RESPONSE",
			trace.WithAttributes(
				attribute.Int(
					"http.status_code",
					http.StatusMethodNotAllowed,
				),
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

		return
	}

	// =================================
	// 1. AMBIL DATA ESP8266
	// =================================

	span.AddEvent("STEP 1 - GET SENSOR ESP8266")

	sensorData, err := getSensorFromESP8266(ctx)

	if err != nil {

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

		span.AddEvent(
			"ESP8266 FAILED",
			trace.WithAttributes(
				attribute.String(
					"error",
					err.Error(),
				),
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

		return
	}

	// =================================
	// SENSOR DATA TRACE
	// =================================

	span.AddEvent(
		"SENSOR DATA RECEIVED",
		trace.WithAttributes(
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
		),
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

	span.AddEvent("STEP 2 - SEND DATA TO GEMINI")

	aiResult, err := analyzeWithGemini(
		ctx,
		sensorData,
	)

	if err != nil {

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

		span.AddEvent(
			"GEMINI FAILED",
			trace.WithAttributes(
				attribute.String(
					"error",
					err.Error(),
				),
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

		return
	}

	// =================================
	// AI RESULT TRACE
	// =================================

	span.AddEvent(
		"AI ANALYSIS RECEIVED",
		trace.WithAttributes(
			attribute.String(
				"ai.kondisi",
				aiResult.Kondisi,
			),
			attribute.Bool(
				"ai.bahaya",
				aiResult.Bahaya,
			),
			attribute.String(
				"ai.analisis",
				aiResult.Analisis,
			),
			attribute.String(
				"ai.saran",
				aiResult.Saran,
			),
		),
	)

	span.SetAttributes(
		attribute.String(
			"ai.kondisi",
			aiResult.Kondisi,
		),
		attribute.Bool(
			"ai.bahaya",
			aiResult.Bahaya,
		),
	)

	// =================================
	// 3. RESPONSE
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

	span.SetAttributes(
		attribute.Int(
			"http.response.status_code",
			http.StatusOK,
		),
		attribute.Int64(
			"http.response.duration_ms",
			time.Since(start).Milliseconds(),
		),
	)

	span.SetStatus(
		codes.Ok,
		"request berhasil",
	)

	span.AddEvent(
		"HTTP RESPONSE",
		trace.WithAttributes(
			attribute.Int(
				"http.status_code",
				http.StatusOK,
			),
			attribute.Int64(
				"duration_ms",
				time.Since(start).Milliseconds(),
			),
		),
	)
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

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	url := ESP8266URL + "/sensor"

	// =================================
	// REQUEST TRACE
	// =================================

	span.SetAttributes(
		attribute.String(
			"http.request.method",
			http.MethodGet,
		),
		attribute.String(
			"server.address",
			ESP8266URL,
		),
		attribute.String(
			"http.url",
			url,
		),
	)

	span.AddEvent(
		"ESP8266 REQUEST",
		trace.WithAttributes(
			attribute.String(
				"method",
				http.MethodGet,
			),
			attribute.String(
				"url",
				url,
			),
		),
	)

	start := time.Now()

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

		span.AddEvent(
			"ESP8266 REQUEST ERROR",
			trace.WithAttributes(
				attribute.String(
					"error",
					err.Error(),
				),
			),
		)

		return data, err
	}

	// =================================
	// SEND REQUEST
	// =================================

	resp, err := client.Do(req)

	duration := time.Since(start)

	if err != nil {

		span.RecordError(err)

		span.SetStatus(
			codes.Error,
			"ESP8266 request gagal",
		)

		span.SetAttributes(
			attribute.Int64(
				"http.response.duration_ms",
				duration.Milliseconds(),
			),
		)

		span.AddEvent(
			"ESP8266 ERROR",
			trace.WithAttributes(
				attribute.String(
					"error",
					err.Error(),
				),
				attribute.Int64(
					"duration_ms",
					duration.Milliseconds(),
				),
			),
		)

		return data, err
	}

	defer resp.Body.Close()

	// =================================
	// RESPONSE TRACE
	// =================================

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

	span.AddEvent(
		"ESP8266 RESPONSE",
		trace.WithAttributes(
			attribute.Int(
				"http.status_code",
				resp.StatusCode,
			),
			attribute.Int64(
				"duration_ms",
				duration.Milliseconds(),
			),
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

		span.AddEvent(
			"ESP8266 HTTP ERROR",
			trace.WithAttributes(
				attribute.Int(
					"http.status_code",
					resp.StatusCode,
				),
			),
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

		span.AddEvent(
			"ESP8266 READ RESPONSE ERROR",
			trace.WithAttributes(
				attribute.String(
					"error",
					err.Error(),
				),
			),
		)

		return data, err
	}

	// =================================
	// RESPONSE BODY TRACE
	// =================================

	span.AddEvent(
		"ESP8266 RESPONSE BODY",
		trace.WithAttributes(
			attribute.String(
				"body",
				string(responseBody),
			),
		),
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

		span.AddEvent(
			"ESP8266 JSON PARSE ERROR",
			trace.WithAttributes(
				attribute.String(
					"error",
					err.Error(),
				),
			),
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

		span.AddEvent(
			"ESP8266 APPLICATION ERROR",
			trace.WithAttributes(
				attribute.String(
					"message",
					espResponse.Message,
				),
			),
		)

		return data, err
	}

	// =================================
	// SENSOR DATA
	// =================================

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

	span.AddEvent(
		"ESP8266 SENSOR DATA",
		trace.WithAttributes(
			attribute.Float64(
				"suhu",
				espResponse.Data.Suhu,
			),
			attribute.Float64(
				"kelembapan",
				espResponse.Data.Kelembapan,
			),
			attribute.String(
				"status",
				espResponse.Data.Status,
			),
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
	// SENSOR DATA TRACE
	// =================================

	span.SetAttributes(
		attribute.Float64(
			"sensor.suhu",
			sensor.Suhu,
		),
		attribute.Float64(
			"sensor.kelembapan",
			sensor.Kelembapan,
		),
		attribute.String(
			"sensor.status",
			sensor.Status,
		),
	)

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

		span.AddEvent(
			"GEMINI API KEY ERROR",
			trace.WithAttributes(
				attribute.String(
					"error",
					err.Error(),
				),
			),
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

	// Jangan pernah memasukkan API key
	// ke Jaeger attribute/event.

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

	// =================================
	// GEMINI REQUEST TRACE
	// =================================

	span.AddEvent(
		"GEMINI REQUEST",
		trace.WithAttributes(
			attribute.String(
				"method",
				http.MethodPost,
			),
			attribute.String(
				"url",
				GEMINIURL,
			),
			attribute.String(
				"content_type",
				"application/json",
			),
		),
	)

	client := &http.Client{
		Timeout: 20 * time.Second,
	}

	start := time.Now()

	// =================================
	// SEND REQUEST
	// =================================

	resp, err := client.Do(req)

	duration := time.Since(start)

	if err != nil {

		span.RecordError(err)

		span.SetStatus(
			codes.Error,
			"Gemini request gagal",
		)

		span.SetAttributes(
			attribute.Int64(
				"http.response.duration_ms",
				duration.Milliseconds(),
			),
		)

		span.AddEvent(
			"GEMINI ERROR",
			trace.WithAttributes(
				attribute.String(
					"error",
					err.Error(),
				),
				attribute.Int64(
					"duration_ms",
					duration.Milliseconds(),
				),
			),
		)

		return result, err
	}

	defer resp.Body.Close()

	// =================================
	// RESPONSE TRACE
	// =================================

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

	span.AddEvent(
		"GEMINI RESPONSE",
		trace.WithAttributes(
			attribute.Int(
				"http.status_code",
				resp.StatusCode,
			),
			attribute.Int64(
				"duration_ms",
				duration.Milliseconds(),
			),
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

		span.AddEvent(
			"GEMINI READ RESPONSE ERROR",
			trace.WithAttributes(
				attribute.String(
					"error",
					err.Error(),
				),
			),
		)

		return result, err
	}

	// =================================
	// RESPONSE BODY TRACE
	// =================================

	span.AddEvent(
		"GEMINI RESPONSE BODY",
		trace.WithAttributes(
			attribute.String(
				"body",
				string(responseBody),
			),
		),
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

	span.AddEvent(
		"AI ANSWER RECEIVED",
		trace.WithAttributes(
			attribute.String(
				"answer",
				answer,
			),
		),
	)

	// =================================
	// PARSE JSON GEMINI
	// =================================

	err = json.Unmarshal(
		[]byte(answer),
		&result,
	)

	if err != nil {

		parseErr := fmt.Errorf(
			"response Gemini bukan JSON valid: %w",
			err,
		)

		span.RecordError(parseErr)

		span.SetStatus(
			codes.Error,
			"Gemini response JSON invalid",
		)

		span.AddEvent(
			"GEMINI JSON PARSE ERROR",
			trace.WithAttributes(
				attribute.String(
					"error",
					parseErr.Error(),
				),
			),
		)

		return result, parseErr
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
		attribute.String(
			"ai.analisis",
			result.Analisis,
		),
		attribute.String(
			"ai.saran",
			result.Saran,
		),
	)

	span.AddEvent(
		"AI ANALYSIS RESULT",
		trace.WithAttributes(
			attribute.String(
				"kondisi",
				result.Kondisi,
			),
			attribute.Bool(
				"bahaya",
				result.Bahaya,
			),
			attribute.String(
				"analisis",
				result.Analisis,
			),
			attribute.String(
				"saran",
				result.Saran,
			),
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
