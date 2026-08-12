package app

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func loginPage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Sign In · CareLock Consulting</title>
  <style>
    body { margin:0; min-height:100vh; display:flex; align-items:center; justify-content:center; font-family: Arial, sans-serif; background:#0b1220; color:#e5e7eb; }
    main { width:100%; max-width:380px; padding:28px; background:#111827; border:1px solid #334155; border-radius:16px; }
    h1 { margin:0 0 6px; font-size:20px; }
    p.sub { margin:0 0 18px; color:#94a3b8; font-size:13px; }
    label { display:block; margin:14px 0 6px; font-size:13px; color:#cbd5e1; }
    input { width:100%; box-sizing:border-box; padding:10px 12px; border:1px solid #334155; border-radius:10px; background:#0f172a; color:#e2e8f0; font:inherit; }
    button { width:100%; margin-top:20px; padding:10px 12px; border-radius:10px; border:1px solid #334155; background:#2563eb; color:#fff; font:inherit; font-weight:600; cursor:pointer; }
    button:hover { background:#1d4ed8; }
    .status { margin-top:14px; font-size:13px; color:#f87171; min-height:18px; }
  </style>
</head>
<body>
  <main>
    <h1>Sign In</h1>
    <p class="sub">Use your auth username and password.</p>
    <form id="loginForm">
      <label for="username">Username</label>
      <input id="username" name="username" autocomplete="username" required>
      <label for="password">Password</label>
      <input id="password" name="password" type="password" autocomplete="current-password" required>
      <button type="submit">Sign In</button>
      <div class="status" id="status"></div>
    </form>
  </main>
  <script>
    const statusEl = document.getElementById('status');
    document.getElementById('loginForm').addEventListener('submit', async (e) => {
      e.preventDefault();
      statusEl.textContent = '';
      const username = document.getElementById('username').value.trim();
      const password = document.getElementById('password').value;
      const res = await fetch('/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password })
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        statusEl.textContent = body.error || 'Sign in failed';
        return;
      }
      const params = new URLSearchParams(window.location.search);
      const next = params.get('next');
      window.location.href = (next && next.startsWith('/')) ? next : '/';
    });
  </script>
</body>
</html>`
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}
