package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Login opens a browser login page, waits for the user to authenticate,
// registers the device, saves credentials, and returns them.
func Login() (*Credentials, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start local server: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	type result struct {
		creds *Credentials
		err   error
	}
	ch := make(chan result, 1)

	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(loginHTML))
	})

	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ServerURL string `json:"server_url"`
			Email     string `json:"email"`
			Password  string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.Password == "" || req.ServerURL == "" {
			http.Error(w, "all fields are required", http.StatusBadRequest)
			return
		}
		creds, err := doLogin(normalizeHTTP(req.ServerURL), req.Email, req.Password)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		select {
		case ch <- result{creds: creds}:
		default:
		}
	})

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			select {
			case ch <- result{err: err}:
			default:
			}
		}
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	fmt.Printf("Opening %s\nIf the browser did not open, visit the URL manually.\n", url)
	openBrowser(url)

	select {
	case res := <-ch:
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		return res.creds, res.err
	case <-time.After(5 * time.Minute):
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		return nil, fmt.Errorf("login timed out after 5 minutes")
	}
}

func doLogin(httpBase, email, password string) (*Credentials, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	// Step 1: authenticate
	loginBody, _ := json.Marshal(map[string]string{"email": email, "password": password})
	loginResp, err := client.Post(httpBase+"/api/auth/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		return nil, fmt.Errorf("connect to server: %w", err)
	}
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("invalid email or password")
	}
	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		UserID       string `json:"user_id"`
	}
	if err := json.NewDecoder(loginResp.Body).Decode(&tokens); err != nil {
		return nil, fmt.Errorf("decode login response: %w", err)
	}

	// Step 2: register this device
	hostname, _ := os.Hostname()
	devBody, _ := json.Marshal(map[string]string{"name": hostname, "platform": runtime.GOOS})
	devReq, _ := http.NewRequest(http.MethodPost, httpBase+"/api/devices", bytes.NewReader(devBody))
	devReq.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	devReq.Header.Set("Content-Type", "application/json")
	devResp, err := client.Do(devReq)
	if err != nil {
		return nil, fmt.Errorf("register device: %w", err)
	}
	defer devResp.Body.Close()
	if devResp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("device registration failed (status %d)", devResp.StatusCode)
	}
	var dev struct {
		DeviceID    string `json:"device_id"`
		DeviceToken string `json:"device_token"`
	}
	if err := json.NewDecoder(devResp.Body).Decode(&dev); err != nil {
		return nil, fmt.Errorf("decode device response: %w", err)
	}

	return &Credentials{
		ServerURL:    httpBase,
		UserID:       tokens.UserID,
		Email:        email,
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		DeviceID:     dev.DeviceID,
		DeviceToken:  dev.DeviceToken,
	}, nil
}

// normalizeHTTP converts ws(s):// → http(s):// and strips trailing slashes.
func normalizeHTTP(u string) string {
	u = strings.TrimRight(u, "/")
	u = strings.ReplaceAll(u, "ws://", "http://")
	u = strings.ReplaceAll(u, "wss://", "https://")
	return u
}

// HTTPToWS converts http(s):// → ws(s):// for WebSocket connections.
func HTTPToWS(u string) string {
	u = strings.ReplaceAll(u, "http://", "ws://")
	u = strings.ReplaceAll(u, "https://", "wss://")
	return u
}

func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{url}
	case "windows":
		cmd, args = "cmd", []string{"/c", "start", url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	_ = exec.Command(cmd, args...).Start()
}

const loginHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>ccmux — Sign in</title>
<style>
  *{box-sizing:border-box;margin:0;padding:0}
  body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:#0f1117;color:#e2e8f0;display:flex;align-items:center;justify-content:center;min-height:100vh}
  .card{background:#1a1d27;border:1px solid #2d3148;border-radius:12px;padding:40px;width:380px}
  h1{font-size:22px;font-weight:600;margin-bottom:6px}
  .sub{color:#64748b;font-size:14px;margin-bottom:28px}
  label{display:block;font-size:13px;color:#94a3b8;margin-bottom:5px}
  input{display:block;width:100%;padding:10px 14px;background:#0f1117;border:1px solid #2d3148;border-radius:8px;color:#e2e8f0;font-size:14px;margin-bottom:18px;outline:none;transition:border-color .15s}
  input:focus{border-color:#6366f1}
  button{width:100%;padding:12px;background:#6366f1;color:#fff;border:none;border-radius:8px;font-size:15px;font-weight:500;cursor:pointer;transition:background .15s}
  button:hover{background:#5558e3}
  button:disabled{background:#374151;cursor:not-allowed}
  .err{color:#f87171;font-size:13px;margin-top:14px;display:none}
  .ok{text-align:center}
  .ok h2{color:#34d399;font-size:20px;margin-bottom:10px}
  .ok p{color:#94a3b8;font-size:14px}
</style>
</head>
<body>
<div class="card">
  <div id="form">
    <h1>Sign in to ccmux</h1>
    <p class="sub">Connect this device to your account.</p>
    <label>Server URL</label>
    <input id="server" type="text" placeholder="http://your-server:8080">
    <label>Email</label>
    <input id="email" type="email" placeholder="you@example.com">
    <label>Password</label>
    <input id="pass" type="password" placeholder="••••••••">
    <button id="btn" onclick="login()">Sign in</button>
    <div class="err" id="err"></div>
  </div>
  <div id="ok" class="ok" style="display:none">
    <h2>Logged in!</h2>
    <p>You can close this window and return to the terminal.</p>
  </div>
</div>
<script>
async function login(){
  const btn=document.getElementById('btn'),errEl=document.getElementById('err');
  const server=document.getElementById('server').value.trim();
  const email=document.getElementById('email').value.trim();
  const pass=document.getElementById('pass').value;
  errEl.style.display='none';
  if(!server||!email||!pass){show('All fields are required.');return;}
  btn.disabled=true;btn.textContent='Signing in…';
  try{
    const r=await fetch('/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({server_url:server,email,password:pass})});
    if(!r.ok){show(await r.text());btn.disabled=false;btn.textContent='Sign in';return;}
    document.getElementById('form').style.display='none';
    document.getElementById('ok').style.display='';
  }catch(e){show(e.message);btn.disabled=false;btn.textContent='Sign in';}
}
function show(m){const e=document.getElementById('err');e.textContent=m;e.style.display='block';}
document.addEventListener('keydown',e=>{if(e.key==='Enter')login();});
</script>
</body>
</html>`
