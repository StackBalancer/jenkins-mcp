package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Minimal Jenkins client
type JenkinsClient struct {
	Base  string
	User  string
	Token string
}

func (jc *JenkinsClient) do(method, path string, params map[string]string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, jc.Base+path, body)
	if err != nil {
		return nil, err
	}
	if jc.User != "" || jc.Token != "" {
		req.SetBasicAuth(jc.User, jc.Token)
	}

	q := req.URL.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	req.URL.RawQuery = q.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("jenkins error: status=%d body=%s", resp.StatusCode, string(respBytes))
	}
	return respBytes, nil
}

// TriggerJob wraps jc.do() for build and buildWithParameters
func (jc *JenkinsClient) TriggerJob(jobName string, params map[string]string) error {
	path := fmt.Sprintf("/job/%s/build", jobName)
	if len(params) > 0 {
		path = fmt.Sprintf("/job/%s/buildWithParameters", jobName)
	}

	log.Printf("[DEBUG] Triggering job at path: %s with params: %+v", path, params)

	_, err := jc.do("POST", path, params, nil)
	if err != nil {
		return fmt.Errorf("failed to trigger job: %w", err)
	}
	return nil
}

func main() {
	// Jenkins access details
	jenkinsURL := os.Getenv("JENKINS_URL")
	if jenkinsURL == "" {
		jenkinsURL = "http://jenkins:8080/jenkins"
	}
	mcpTokenBytes, err := os.ReadFile(os.Getenv("JENKINS_TOKEN_FILE"))
	if err != nil {
		log.Fatalf("failed to read MCP Jenkins token: %v", err)
	}
	mcpJenkinsToken := strings.TrimSpace(string(mcpTokenBytes))

	jc := &JenkinsClient{
		Base:  jenkinsURL,
		User:  os.Getenv("JENKINS_MCP_USER"),
		Token: mcpJenkinsToken,
	}

	// MCP server
	m := server.NewMCPServer("jenkins-mcp", "1.0.0")

	// trigger_job tool
	triggerTool := mcp.NewTool(
		"trigger_job",
		mcp.WithDescription("Trigger a Jenkins job by name; optionally pass parameters (map)."),
		mcp.WithString("job_name", mcp.Description("job name (required)"), mcp.Required()),
		mcp.WithObject("parameters", mcp.Description("optional key/value parameters")),
	)
	m.AddTool(triggerTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		jobNameVal, ok := req.Params.Arguments.(map[string]any)["job_name"].(string)
		if !ok || jobNameVal == "" {
			return mcp.NewToolResultError("job_name is required"), nil
		}

		var params map[string]string
		if argMap, ok := req.Params.Arguments.(map[string]any); ok {
			if p, exists := argMap["parameters"].(map[string]any); exists {
				params = make(map[string]string)
				for k, v := range p {
					params[k] = fmt.Sprint(v)
				}
			}
		}

		if err := jc.TriggerJob(jobNameVal, params); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText("job triggered successfully!"), nil
	})

	// get_build_status tool
	statusTool := mcp.NewTool(
		"get_build_status",
		mcp.WithDescription("Get latest build status for job (returns raw JSON)."),
		mcp.WithString("job_name", mcp.Description("job name (required)"), mcp.Required()),
	)
	m.AddTool(statusTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		jobNameVal, _ := req.Params.Arguments.(map[string]any)["job_name"].(string)
		data, err := jc.do("GET", fmt.Sprintf("/job/%s/lastBuild/api/json", jobNameVal), nil, nil)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var parsed any
		if err := json.Unmarshal(data, &parsed); err != nil {
			return mcp.NewToolResultText(string(data)), nil
		}
		return mcp.NewToolResultStructuredOnly(parsed), nil
	})

	// get_console_log tool
	consoleTool := mcp.NewTool(
		"get_console_log",
		mcp.WithDescription("Get console log for job/build number (build_number required)."),
		mcp.WithString("job_name", mcp.Description("job name (required)"), mcp.Required()),
		mcp.WithString("build_number", mcp.Description("build number (required)"), mcp.Required()),
	)
	m.AddTool(consoleTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		jobNameVal, _ := req.Params.Arguments.(map[string]any)["job_name"].(string)

		var buildNumber int
		if val, ok := req.Params.Arguments.(map[string]any)["build_number"]; ok {
			switch v := val.(type) {
			case int:
				buildNumber = v
			case float64:
				buildNumber = int(v)
			case string:
				n, err := strconv.Atoi(v)
				if err != nil {
					return mcp.NewToolResultError("invalid build_number string"), nil
				}
				buildNumber = n
			default:
				return mcp.NewToolResultError("invalid build_number type"), nil
			}
		} else {
			return mcp.NewToolResultError("missing build_number"), nil
		}

		data, err := jc.do("GET", fmt.Sprintf("/job/%s/%d/consoleText", jobNameVal, buildNumber), nil, nil)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		logText := string(data)
		if len(logText) > 100000 {
			logText = logText[:100000] + "\n...(truncated)"
		}
		return mcp.NewToolResultText(logText), nil
	})

	// Start SSE server
	sse := server.NewSSEServer(m)
	log.Printf("starting SSE server on :8081")
	if err := sse.Start(":8081"); err != nil {
		log.Fatalf("sse server failed: %v", err)
	}
}
