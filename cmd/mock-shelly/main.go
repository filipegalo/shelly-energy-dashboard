package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

func main() {
	profile := env("MOCK_PROFILE", "gen2")
	address := env("MOCK_LISTEN_ADDRESS", "0.0.0.0:80")
	var energyReadingCount atomic.Uint64
	mux := http.NewServeMux()
	requireDigest := func(w http.ResponseWriter, r *http.Request) bool {
		if profile != "auth" || r.Header.Get("Authorization") != "" {
			return false
		}
		w.Header().Set("WWW-Authenticate", `Digest realm="shelly", nonce="mock-nonce", algorithm=SHA-256, qop="auth"`)
		w.WriteHeader(http.StatusUnauthorized)
		return true
	}
	mux.HandleFunc("/shelly", func(w http.ResponseWriter, r *http.Request) {
		if profile == "offline" || (profile == "energy-flaky" && energyReadingCount.Load() >= 2) {
			if hijacker, ok := w.(http.Hijacker); ok {
				connection, _, _ := hijacker.Hijack()
				if connection != nil {
					connection.Close()
				}
			}
			return
		}
		if profile == "slow" {
			time.Sleep(5 * time.Second)
		}
		if profile == "malformed" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch profile {
		case "gen1":
			json.NewEncoder(w).Encode(map[string]any{"type": "SHSW-1", "mac": "AABBCCDDEE01", "auth": false, "fw": "20230913-112003/v1.14.0"})
		case "gen3":
			json.NewEncoder(w).Encode(map[string]any{"id": "shelly1g3-mock03", "model": "S3SW-001X16EU", "gen": 3, "mac": "AABBCCDDEE03", "ver": "1.5.0", "app": "Switch"})
		case "auth":
			json.NewEncoder(w).Encode(map[string]any{"id": "shellyplus1-auth", "model": "SNSW-001X16EU", "gen": 2, "mac": "AABBCCDDEE04", "ver": "1.4.4", "auth_en": true})
		case "ipchange":
			json.NewEncoder(w).Encode(map[string]any{"id": "shellyplus1-moving", "model": "SNSW-001X16EU", "gen": 2, "mac": "AABBCCDDEE09", "ver": "1.4.4"})
		case "energy", "energy-reset", "energy-invalid", "energy-slow", "energy-flaky":
			id, mac := energyIdentity(profile)
			json.NewEncoder(w).Encode(map[string]any{"id": id, "model": "SPEM-003CEBEU", "gen": 2, "mac": mac, "ver": "1.4.4", "app": "Pro3EM"})
		default:
			json.NewEncoder(w).Encode(map[string]any{"id": "shellyplus1-mock02", "model": "SNSW-001X16EU", "gen": 2, "mac": "AABBCCDDEE02", "ver": "1.4.4", "app": "Plus1"})
		}
	})
	mux.HandleFunc("/settings", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"name": "Mock Gen1", "device": map[string]any{"hostname": "shelly1-mock01", "type": "SHSW-1", "mac": "AABBCCDDEE01"}})
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"relays": []any{map[string]any{"ison": true}}, "meters": []any{map[string]any{"power": 12.3}}})
	})
	mux.HandleFunc("/rpc/Sys.GetConfig", func(w http.ResponseWriter, r *http.Request) {
		if requireDigest(w, r) {
			return
		}
		if profile == "unsupported" {
			http.Error(w, "method not found", http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"device": map[string]any{"name": "Mock " + profile}})
	})
	mux.HandleFunc("/rpc/Shelly.GetDeviceInfo", func(w http.ResponseWriter, r *http.Request) {
		if requireDigest(w, r) {
			return
		}
		if profile == "unsupported" {
			http.Error(w, "method not found", 404)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"name": "Mock " + profile})
	})
	mux.HandleFunc("/rpc/Shelly.GetStatus", func(w http.ResponseWriter, r *http.Request) {
		if requireDigest(w, r) {
			return
		}
		if isEnergyProfile(profile) {
			json.NewEncoder(w).Encode(map[string]any{"sys": map[string]any{"uptime": 1234}, "em:0": energyLiveStatus(profile, energyReadingCount.Load()), "emdata:0": energyDataStatus(profile, energyReadingCount.Load())})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"sys": map[string]any{"uptime": 1234}, "switch:0": map[string]any{"output": true, "apower": 8.2, "voltage": 230.1}, "input:0": map[string]any{"state": false}})
	})
	mux.HandleFunc("/rpc/EM.GetStatus", func(w http.ResponseWriter, r *http.Request) {
		if requireDigest(w, r) {
			return
		}
		if profile == "energy-slow" {
			time.Sleep(5 * time.Second)
		}
		json.NewEncoder(w).Encode(energyLiveStatus(profile, energyReadingCount.Load()))
	})
	mux.HandleFunc("/rpc/EMData.GetStatus", func(w http.ResponseWriter, r *http.Request) {
		if requireDigest(w, r) {
			return
		}
		step := energyReadingCount.Add(1)
		json.NewEncoder(w).Encode(energyDataStatus(profile, step))
	})
	mux.HandleFunc("/rpc/Sys.GetStatus", func(w http.ResponseWriter, r *http.Request) {
		if requireDigest(w, r) {
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"uptime": 1234})
	})
	log.Printf("mock Shelly profile=%s listening=%s", profile, address)
	log.Fatal(http.ListenAndServe(address, mux))
}

func isEnergyProfile(profile string) bool {
	switch profile {
	case "energy", "energy-reset", "energy-invalid", "energy-slow", "energy-flaky":
		return true
	default:
		return false
	}
}

func energyIdentity(profile string) (string, string) {
	identities := map[string]string{"energy": "11", "energy-reset": "12", "energy-invalid": "13", "energy-slow": "14", "energy-flaky": "15"}
	suffix := identities[profile]
	return "shellypro3em-" + suffix, "AABBCCDDEE" + suffix
}

func energyLiveStatus(profile string, step uint64) map[string]any {
	if profile == "energy-invalid" {
		return map[string]any{"id": 0, "errors": []string{"ct_type_not_set"}}
	}
	power := 1200.0 + float64(step%20)*10
	return map[string]any{"id": 0, "a_act_power": power * .32, "b_act_power": power * .33, "c_act_power": power * .35, "total_act_power": power, "a_voltage": 230.1, "b_voltage": 229.8, "c_voltage": 230.4, "a_current": 1.8, "b_current": 1.9, "c_current": 2.0, "a_pf": .98, "b_pf": .97, "c_pf": .99, "frequency": 50.0}
}

func energyDataStatus(profile string, step uint64) map[string]any {
	total := 100000.0 + float64(step)*10
	if profile == "energy-reset" && step >= 3 {
		total = float64(step-3) * 10
	}
	return map[string]any{"id": 0, "total_act": total, "total_act_ret": float64(step), "a_total_act_energy": total * .32, "b_total_act_energy": total * .33, "c_total_act_energy": total * .35}
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
