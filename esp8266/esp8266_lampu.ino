#include <ESP8266WiFi.h>
#include <ESP8266WebServer.h>
#include <ArduinoJson.h>
#include <DHT.h>
#include <Wire.h>
#include <LiquidCrystal_I2C.h>
#include "secrets.h"

// =====================================
// KONFIGURASI WIFI
// =====================================


// =====================================
// KONFIGURASI PIN
// =====================================

// LED
const int LED_MERAH  = D3;
const int LED_KUNING = D5;
const int LED_HIJAU  = D6;

// DHT11
#define DHT_PIN D4
#define DHT_TYPE DHT11

DHT dht(DHT_PIN, DHT_TYPE);

// LCD 16x2 I2C
// Alamat umum: 0x27
LiquidCrystal_I2C lcd(0x27, 16, 2);

// Web server
ESP8266WebServer server(80);

// =====================================
// BATAS KONDISI
// =====================================

// AMAN
const float SUHU_MIN_AMAN = 20.0;
const float SUHU_MAX_AMAN = 28.0;

const float HUM_MIN_AMAN = 50.0;
const float HUM_MAX_AMAN = 70.0;

// BAHAYA
const float SUHU_MIN_BAHAYA = 15.0;
const float SUHU_MAX_BAHAYA = 32.0;

const float HUM_MIN_BAHAYA = 40.0;
const float HUM_MAX_BAHAYA = 80.0;

// =====================================
// VARIABLE SENSOR
// =====================================

float suhu = 0;
float kelembapan = 0;

String statusLingkungan = "UNKNOWN";

// =====================================
// SETUP
// =====================================

void setup() {

  Serial.begin(115200);

  delay(1000);

  Serial.println();
  Serial.println("=================================");
  Serial.println(" ESP8266 IoT Peternakan");
  Serial.println("=================================");

  // LED
  pinMode(LED_MERAH, OUTPUT);
  pinMode(LED_KUNING, OUTPUT);
  pinMode(LED_HIJAU, OUTPUT);

  matikanSemuaLampu();

  // DHT
  dht.begin();

  // LCD
  Wire.begin(D2, D1);

  lcd.init();
  lcd.backlight();

  lcd.clear();
  lcd.setCursor(0, 0);
  lcd.print("IoT Peternakan");
  lcd.setCursor(0, 1);
  lcd.print("Starting...");

  // WiFi
  connectWiFi();

  // Endpoint lama
  server.on("/lampu", HTTP_POST, handleLampu);

  // Health
  server.on("/health", HTTP_GET, handleHealth);

  // Sensor
  server.on("/sensor", HTTP_GET, handleSensor);

  server.begin();

  Serial.println();
  Serial.println("HTTP Server started!");
  Serial.print("ESP8266 IP Address: ");
  Serial.println(WiFi.localIP());

  delay(1000);

  // Baca sensor pertama kali
  bacaSensor();
}

// =====================================
// LOOP
// =====================================

void loop() {

  server.handleClient();

  static unsigned long lastSensorRead = 0;

  if (millis() - lastSensorRead >= 2000) {

    lastSensorRead = millis();

    bacaSensor();
  }
}

// =====================================
// WIFI
// =====================================

void connectWiFi() {

  Serial.print("Connecting to WiFi");

  WiFi.begin(WIFI_SSID, WIFI_PASSWORD);

  while (WiFi.status() != WL_CONNECTED) {

    delay(500);

    Serial.print(".");
  }

  Serial.println();
  Serial.println("WiFi connected!");

  Serial.print("IP Address: ");
  Serial.println(WiFi.localIP());
}

// =====================================
// BACA SENSOR
// =====================================

void bacaSensor() {

  float h = dht.readHumidity();
  float t = dht.readTemperature();

  // Cek apakah pembacaan valid
  if (isnan(h) || isnan(t)) {

    Serial.println("Gagal membaca DHT11!");

    lcd.clear();
    lcd.setCursor(0, 0);
    lcd.print("Sensor Error");
    lcd.setCursor(0, 1);
    lcd.print("Cek DHT11");

    return;
  }

  suhu = t;
  kelembapan = h;

  Serial.println();
  Serial.println("=================================");
  Serial.print("Suhu       : ");
  Serial.print(suhu);
  Serial.println(" C");

  Serial.print("Kelembapan : ");
  Serial.print(kelembapan);
  Serial.println(" %");

  // Tentukan status
  tentukanStatus();

  // Update LED
  updateLampuStatus();

  // Update LCD
  updateLCD();
}

// =====================================
// TENTUKAN STATUS
// =====================================

void tentukanStatus() {

  bool suhuAman =
      suhu >= SUHU_MIN_AMAN &&
      suhu <= SUHU_MAX_AMAN;

  bool humAman =
      kelembapan >= HUM_MIN_AMAN &&
      kelembapan <= HUM_MAX_AMAN;

  bool suhuBahaya =
      suhu < SUHU_MIN_BAHAYA ||
      suhu > SUHU_MAX_BAHAYA;

  bool humBahaya =
      kelembapan < HUM_MIN_BAHAYA ||
      kelembapan > HUM_MAX_BAHAYA;

  // ================================
  // MERAH
  // ================================

  if (suhuBahaya || humBahaya) {

    statusLingkungan = "BAHAYA";

  }

  // ================================
  // HIJAU
  // ================================

  else if (suhuAman && humAman) {

    statusLingkungan = "AMAN";

  }

  // ================================
  // KUNING
  // ================================

  else {

    statusLingkungan = "WASPADA";
  }

  Serial.print("Status     : ");
  Serial.println(statusLingkungan);
}

// =====================================
// UPDATE LED
// =====================================

void updateLampuStatus() {

  matikanSemuaLampu();

  if (statusLingkungan == "AMAN") {

    digitalWrite(LED_HIJAU, HIGH);

  }

  else if (statusLingkungan == "WASPADA") {

    digitalWrite(LED_KUNING, HIGH);

  }

  else if (statusLingkungan == "BAHAYA") {

    digitalWrite(LED_MERAH, HIGH);
  }
}

// =====================================
// UPDATE LCD
// =====================================

void updateLCD() {

  lcd.clear();

  // Baris 1
  lcd.setCursor(0, 0);

  lcd.print("T:");
  lcd.print(suhu, 1);
  lcd.print((char)223);
  lcd.print("C ");

  lcd.print("H:");
  lcd.print(kelembapan, 0);
  lcd.print("%");

  // Baris 2
  lcd.setCursor(0, 1);

  if (statusLingkungan == "AMAN") {

    lcd.print("STATUS: AMAN");

  }

  else if (statusLingkungan == "WASPADA") {

    lcd.print("STATUS: WASPADA");

  }

  else {

    lcd.print("STATUS: BAHAYA");
  }
}

// =====================================
// MATIKAN SEMUA LAMPU
// =====================================

void matikanSemuaLampu() {

  digitalWrite(LED_MERAH, LOW);
  digitalWrite(LED_KUNING, LOW);
  digitalWrite(LED_HIJAU, LOW);
}

// =====================================
// /health
// =====================================

void handleHealth() {

  StaticJsonDocument<200> response;

  response["success"] = true;
  response["message"] = "ESP8266 is running";

  sendJsonResponse(200, response);
}

// =====================================
// /sensor
// =====================================

void handleSensor() {

  StaticJsonDocument<400> response;

  response["success"] = true;

  JsonObject data =
      response.createNestedObject("data");

  data["suhu"] = suhu;
  data["kelembapan"] = kelembapan;
  data["status"] = statusLingkungan;

  sendJsonResponse(200, response);
}

// =====================================
// /lampu
// =====================================

void handleLampu() {

  if (!server.hasArg("plain")) {

    StaticJsonDocument<200> response;

    response["success"] = false;
    response["message"] = "request body kosong";

    sendJsonResponse(400, response);

    return;
  }

  String body = server.arg("plain");

  Serial.println();
  Serial.println("=================================");
  Serial.println("Request diterima:");
  Serial.println(body);

  StaticJsonDocument<300> request;

  DeserializationError error =
      deserializeJson(request, body);

  if (error) {

    Serial.print("JSON error: ");
    Serial.println(error.c_str());

    StaticJsonDocument<200> response;

    response["success"] = false;
    response["message"] = "JSON tidak valid";

    sendJsonResponse(400, response);

    return;
  }

  bool isMerah =
      request["isMerah"] | false;

  bool isKuning =
      request["isKuning"] | false;

  bool isHijau =
      request["isHijau"] | false;

  int jumlahLampu = 0;

  if (isMerah) jumlahLampu++;
  if (isKuning) jumlahLampu++;
  if (isHijau) jumlahLampu++;

  if (jumlahLampu > 1) {

    StaticJsonDocument<200> response;

    response["success"] = false;
    response["message"] =
        "hanya satu lampu yang boleh menyala";

    sendJsonResponse(400, response);

    return;
  }

  setLampu(
      isMerah,
      isKuning,
      isHijau
  );

  StaticJsonDocument<300> response;

  response["success"] = true;
  response["message"] =
      "lampu berhasil diubah";

  JsonObject data =
      response.createNestedObject("data");

  data["isMerah"] = isMerah;
  data["isKuning"] = isKuning;
  data["isHijau"] = isHijau;

  sendJsonResponse(200, response);
}

// =====================================
// SET LAMPU MANUAL
// =====================================

void setLampu(
    bool merah,
    bool kuning,
    bool hijau
) {

  matikanSemuaLampu();

  if (merah) {
    digitalWrite(LED_MERAH, HIGH);
    Serial.println("LED MERAH: ON");
  }

  if (kuning) {
    digitalWrite(LED_KUNING, HIGH);
    Serial.println("LED KUNING: ON");
  }

  if (hijau) {
    digitalWrite(LED_HIJAU, HIGH);
    Serial.println("LED HIJAU: ON");
  }

  if (!merah && !kuning && !hijau) {
    Serial.println("SEMUA LED: OFF");
  }
}

// =====================================
// SEND JSON
// =====================================

void sendJsonResponse(
    int statusCode,
    JsonDocument& response
) {

  String output;

  serializeJson(response, output);

  server.send(
      statusCode,
      "application/json",
      output
  );
}
