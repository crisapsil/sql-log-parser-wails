package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var reQuery = regexp.MustCompile(`(?i)(.*) \(execution took ([0-9]+) milliseconds\)`)
var rePrefix = regexp.MustCompile(`(?i)^.*?:\s+((?:select|insert|update|delete)\s)`)
var reDirect = regexp.MustCompile(`(?i)^\s*(select|insert|update|delete)\s`)

// App struct — methods on this are exposed to the frontend via window.go.main.App.*
type App struct {
	ctx context.Context
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// ── Types ─────────────────────────────────────────────────────────────────────

type ParseResult struct {
	Rows      string `json:"rows"`       // raw HTML <tr> rows
	Count     int    `json:"count"`
	IsMerge   bool   `json:"isMerge"`
	Error     string `json:"error,omitempty"`
}

type IBMProgress struct {
	Total   int    `json:"total"`
	Current int    `json:"current"`
	Pct     int    `json:"pct"`
	Message string `json:"message"`
}

// ── Core parsing ──────────────────────────────────────────────────────────────

func extractSQL(raw string) string {
	raw = strings.TrimSpace(raw)
	if loc := rePrefix.FindStringIndex(raw); loc != nil {
		match := raw[loc[0]:loc[1]]
		kwIdx := strings.IndexAny(strings.ToLower(match), "siud")
		for kwIdx > 0 && match[kwIdx-1] != ' ' {
			kwIdx--
		}
		return strings.TrimSpace(raw[loc[0]+kwIdx:])
	}
	if reDirect.MatchString(raw) {
		return strings.TrimSpace(raw)
	}
	return ""
}

func parseScanner(scanner *bufio.Scanner, fileName string) (string, int) {
	var buf strings.Builder
	count := 0
	withFile := fileName != ""
	for scanner.Scan() {
		m := reQuery.FindStringSubmatch(scanner.Text())
		if m == nil {
			continue
		}
		sql := extractSQL(m[1])
		if sql == "" {
			continue
		}
		timeMs, _ := strconv.Atoi(m[2])
		esc := html.EscapeString(sql)
		cls := ""
		if timeMs >= 5000 {
			cls = ` class="slow"`
		}
		if withFile {
			buf.WriteString(fmt.Sprintf(
				"<tr>\n<td%s>%d</td>\n<td>%s</td>\n<td><pre>%s</pre></td>\n</tr>\n",
				cls, timeMs, html.EscapeString(fileName), esc,
			))
		} else {
			buf.WriteString(fmt.Sprintf(
				"<tr>\n<td%s>%d</td>\n<td><pre>%s</pre></td>\n</tr>\n",
				cls, timeMs, esc,
			))
		}
		count++
	}
	return buf.String(), count
}

func parseText(text string) (string, int) {
	s := bufio.NewScanner(strings.NewReader(text))
	s.Buffer(make([]byte, 1024*1024), 1024*1024)
	return parseScanner(s, "")
}

func parseFile(path, fileName string) (string, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 1024*1024), 1024*1024)
	rows, count := parseScanner(s, fileName)
	return rows, count, nil
}

// ── HTML builder ──────────────────────────────────────────────────────────────

func buildHTML(rows string, total, slow, avg, maxT int, isMerge bool) string {
	slowClass := ""
	if slow > 0 {
		slowClass = "slow-stat"
	}
	statsHTML := fmt.Sprintf(
		`<div class="stats"><span>Total: <b>%s</b></span><span class="%s">Slow (&gt;=5s): <b>%s</b></span><span>Avg: <b>%s ms</b></span><span>Max: <b>%s ms</b></span></div>`,
		fmtNum(total), slowClass, fmtNum(slow), fmtNum(avg), fmtNum(maxT),
	)
	var header string
	if isMerge {
		header = "<tr>\n<th onclick=\"sortTable(0)\">Time (ms)</th>\n<th>File</th>\n<th>SQL</th>\n</tr>"
	} else {
		header = "<tr>\n<th onclick=\"sortTable(0)\">Time (ms)</th>\n<th>SQL</th>\n</tr>"
	}
	return `<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<title>SQL Queries</title>
<style>
body{font-family:Arial,sans-serif}
h2{margin-bottom:8px}
.stats{display:flex;gap:12px;flex-wrap:wrap;margin-bottom:14px;font-size:13px}
.stats span{background:#f0f0f0;padding:3px 10px;border-radius:10px}
.stats .slow-stat{background:#fdecea;color:#c00}
table{border-collapse:collapse;width:100%}
th,td{border:1px solid #ccc;padding:6px;text-align:left;vertical-align:top}
th{background:#f2f2f2;cursor:pointer}
pre{margin:0;white-space:pre-wrap;word-wrap:break-word}
.slow{color:#b00020;font-weight:bold}
</style>
<script>
let sortDir=false;
function sortTable(col){
  const t=document.getElementById("queryTable");
  const rows=Array.from(t.rows).slice(1);
  rows.sort((a,b)=>{
    const A=parseInt(a.cells[col].innerText)||0;
    const B=parseInt(b.cells[col].innerText)||0;
    return sortDir?A-B:B-A;
  });
  sortDir=!sortDir;
  rows.forEach(r=>t.appendChild(r));
}
</script>
</head>
<body>
<h2>SQL Statements and Execution Time</h2>
` + statsHTML + `
<p style="margin-bottom:8px">Click on <b>Time (ms)</b> header to sort</p>
<table id="queryTable">
` + header + `
` + rows + `</table>
</body>
</html>`
}

func fmtNum(n int) string {
	s := strconv.Itoa(n)
	out := ""
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out += ","
		}
		out += string(c)
	}
	return out
}

// ── Exported methods (called from JS) ────────────────────────────────────────

// ParseFile parses a single log file chosen via native dialog.
func (a *App) ParseFile() ParseResult {
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select log file",
		Filters: []runtime.FileFilter{
			{DisplayName: "Log files (*.log, *.txt)", Pattern: "*.log;*.txt"},
			{DisplayName: "All files", Pattern: "*"},
		},
	})
	if err != nil || len(paths) == 0 {
		return ParseResult{}
	}
	rows, count, err := parseFile(paths[0], "")
	if err != nil {
		return ParseResult{Error: err.Error()}
	}
	return ParseResult{Rows: rows, Count: count, IsMerge: false}
}

// MergeFiles parses multiple log files chosen via native multi-select dialog.
func (a *App) MergeFiles() ParseResult {
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select log files to merge (multi-select supported)",
		Filters: []runtime.FileFilter{
			{DisplayName: "Log files (*.log, *.txt)", Pattern: "*.log;*.txt"},
			{DisplayName: "All files", Pattern: "*"},
		},
	})
	if err != nil || len(paths) == 0 {
		return ParseResult{}
	}
	var allRows strings.Builder
	total := 0
	for _, p := range paths {
		// use only filename, not full path
		parts := strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' })
		name := p
		if len(parts) > 0 {
			name = parts[len(parts)-1]
		}
		rows, count, err := parseFile(p, name)
		if err != nil {
			continue
		}
		allRows.WriteString(rows)
		total += count
	}
	return ParseResult{Rows: allRows.String(), Count: total, IsMerge: true}
}

// ParseText parses pasted text content.
func (a *App) ParseText(text string) ParseResult {
	rows, count := parseText(text)
	return ParseResult{Rows: rows, Count: count, IsMerge: false}
}

// BuildHTML generates the final HTML string from rows + stats.
func (a *App) BuildHTML(rows string, total, slow, avg, maxT int, isMerge bool) string {
	return buildHTML(rows, total, slow, avg, maxT, isMerge)
}

// SaveHTML opens a native Save dialog and writes the HTML file.
func (a *App) SaveHTML(content, defaultName string) string {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save HTML report",
		DefaultFilename: defaultName,
		Filters: []runtime.FileFilter{
			{DisplayName: "HTML files (*.html)", Pattern: "*.html"},
		},
	})
	if err != nil || path == "" {
		return ""
	}
	if !strings.HasSuffix(strings.ToLower(path), ".html") {
		path += ".html"
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "error:" + err.Error()
	}
	return path
}

// ── IBM Editor ────────────────────────────────────────────────────────────────

func convertEditorURL(browserURL string) (string, string, error) {
	parts := strings.SplitN(browserURL, "#", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid URL: expected '#' separator")
	}
	path := parts[1]
	base := strings.SplitN(parts[0], "/sse/", 2)[0]
	apiURL := base + path
	segs := strings.Split(strings.TrimRight(path, "/"), "/")
	name := segs[len(segs)-1]
	if name == "" {
		name = "maximo.log"
	}
	return apiURL, name, nil
}

func ibmLineCount(apiURL, cookie string) (int, error) {
	req, _ := http.NewRequest("GET", apiURL+"?parts=lineCount", nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Orion-Version", "1")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("server returned %d — check URL and cookie", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var result struct {
		LineCount int `json:"LineCount"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("unexpected response: %s", string(body[:min(len(body), 200)]))
	}
	return result.LineCount, nil
}

func ibmFetchPage(apiURL, cookie string, start, count int) (string, error) {
	u, _ := url.Parse(apiURL)
	q := u.Query()
	q.Set("start", strconv.Itoa(start))
	q.Set("count", strconv.Itoa(count))
	u.RawQuery = q.Encode()
	req, _ := http.NewRequest("GET", u.String(), nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Orion-Version", "1")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("server returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

// LoadFromIBM downloads a log from IBM Editor and parses it.
// Progress is emitted as events: "ibm:progress" with IBMProgress JSON.
func (a *App) LoadFromIBM(browserURL, cookie string) ParseResult {
	apiURL, _, err := convertEditorURL(browserURL)
	if err != nil {
		return ParseResult{Error: err.Error()}
	}

	totalLines, err := ibmLineCount(apiURL, cookie)
	if err != nil {
		return ParseResult{Error: err.Error()}
	}

	emit := func(current, pct int, msg string) {
		runtime.EventsEmit(a.ctx, "ibm:progress", IBMProgress{
			Total: totalLines, Current: current, Pct: pct, Message: msg,
		})
	}

	const pageSize = 5000
	var allContent strings.Builder

	for start := 1; start <= totalLines; start += pageSize {
		page, err := ibmFetchPage(apiURL, cookie, start, pageSize)
		if err != nil {
			return ParseResult{Error: err.Error()}
		}
		allContent.WriteString(page)
		done := start + pageSize - 1
		if done > totalLines {
			done = totalLines
		}
		pct := int(float64(done) / float64(totalLines) * 100)
		emit(done, pct, fmt.Sprintf("Downloading... %d / %d lines (%d%%)", done, totalLines, pct))
	}

	rows, count := parseText(allContent.String())
	if count == 0 {
		return ParseResult{Error: "No SQL queries found in log"}
	}
	return ParseResult{Rows: rows, Count: count, IsMerge: false}
}

// SaveRawLog saves raw log content to disk via native save dialog.
func (a *App) SaveRawLog(content, defaultName string) string {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save raw log",
		DefaultFilename: defaultName,
		Filters: []runtime.FileFilter{
			{DisplayName: "Log files (*.log, *.txt)", Pattern: "*.log;*.txt"},
		},
	})
	if err != nil || path == "" {
		return ""
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "error:" + err.Error()
	}
	return path
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
