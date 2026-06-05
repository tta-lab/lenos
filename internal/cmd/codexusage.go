package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	codexpkg "github.com/tta-lab/lenos/internal/agent/codex"
	"github.com/tta-lab/lenos/internal/config"
)

const defaultCodexBaseURL = "https://chatgpt.com/backend-api"

type codexCredentials struct {
	AccessToken string
	AccountID   string
}

var codexUsageCmd = &cobra.Command{
	Use:   "codex-usage",
	Short: "Show Codex weekly usage quota",
	Long: `Fetch and display the Codex (ChatGPT consumer) weekly usage from the
ChatGPT backend API. Reads Codex credentials from your Lenos config.
Requires a configured Codex provider with a valid token (set up via "lenos login").`,
	Example: `  # Show weekly usage
  lenos codex-usage`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, _ := cmd.Flags().GetString("base-url")

		creds, err := loadCodexCredentials()
		if err != nil {
			return err
		}
		if creds.AccessToken == "" {
			return fmt.Errorf("codex access token not found — run 'lenos login' to authenticate")
		}

		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		usage, err := fetchWeeklyUsage(ctx, http.DefaultClient, baseURL, creds.AccessToken, creds.AccountID)
		if err != nil {
			return err
		}

		cmd.Println(formatWeeklyUsage(usage))
		return nil
	},
}

type weeklyUsage struct {
	Plan             string
	UsedPercent      int
	RemainingPercent int
	RefreshAt        time.Time
}

type usageResponse struct {
	PlanType  string          `json:"plan_type"`
	RateLimit rateLimitDetail `json:"rate_limit"`
}

type rateLimitDetail struct {
	SecondaryWindow windowSnapshot `json:"secondary_window"`
}

type windowSnapshot struct {
	UsedPercent int   `json:"used_percent"`
	ResetAt     int64 `json:"reset_at"`
}

// lenosConfigFile mirrors the minimal fields we need from config.json.
type lenosConfigFile struct {
	Providers map[string]lenosProviderConfig `json:"providers"`
}

type lenosProviderConfig struct {
	APIKey       string            `json:"api_key"`
	OAuth        *lenosOAuthToken  `json:"oauth"`
	ExtraHeaders map[string]string `json:"extra_headers"`
}

type lenosOAuthToken struct {
	AccessToken string `json:"access_token"`
}

func loadCodexCredentials() (codexCredentials, error) {
	configPath := config.GlobalConfigData()
	data, err := os.ReadFile(configPath)
	if err != nil {
		return codexCredentials{}, fmt.Errorf("read config: %w — run 'lenos login' first", err)
	}

	var cfg lenosConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return codexCredentials{}, fmt.Errorf("parse config: %w", err)
	}

	provider, ok := cfg.Providers[codexpkg.Name]
	if !ok {
		return codexCredentials{}, fmt.Errorf("codex provider not configured — run 'lenos login' to set it up")
	}

	accessToken := ""
	if provider.OAuth != nil && provider.OAuth.AccessToken != "" {
		accessToken = provider.OAuth.AccessToken
	} else if provider.APIKey != "" {
		accessToken = provider.APIKey
	}

	accountID := ""
	if provider.ExtraHeaders != nil {
		accountID = provider.ExtraHeaders["ChatGPT-Account-Id"]
	}

	return codexCredentials{
		AccessToken: accessToken,
		AccountID:   accountID,
	}, nil
}

func fetchWeeklyUsage(
	ctx context.Context,
	client *http.Client,
	baseURL, accessToken, accountID string,
) (weeklyUsage, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/wham/usage", nil)
	if err != nil {
		return weeklyUsage{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", "lenos-codex-usage")
	req.Header.Set("Accept", "application/json")
	if accountID != "" {
		req.Header.Set("ChatGPT-Account-Id", accountID)
	}

	resp, err := client.Do(req)
	if err != nil {
		return weeklyUsage{}, fmt.Errorf("fetch usage: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return weeklyUsage{}, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return weeklyUsage{}, fmt.Errorf("codex token expired or invalid — run 'lenos login' to re-authenticate")
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return weeklyUsage{}, fmt.Errorf("codex usage API returned %d: %s", resp.StatusCode, string(body))
	}

	var decoded usageResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return weeklyUsage{}, fmt.Errorf("parse response: %w", err)
	}
	weekly := decoded.RateLimit.SecondaryWindow
	if weekly.ResetAt == 0 {
		return weeklyUsage{}, fmt.Errorf("weekly usage window not found in response")
	}

	used := clampPercent(weekly.UsedPercent)
	return weeklyUsage{
		Plan:             decoded.PlanType,
		UsedPercent:      used,
		RemainingPercent: 100 - used,
		RefreshAt:        time.Unix(weekly.ResetAt, 0),
	}, nil
}

func formatWeeklyUsage(usage weeklyUsage) string {
	now := time.Now()
	var b strings.Builder
	if usage.Plan != "" {
		fmt.Fprintf(&b, "Plan: %s\n", usage.Plan)
	}
	fmt.Fprintf(&b, "Weekly usage: %d%% used, %d%% left\n", usage.UsedPercent, usage.RemainingPercent)
	refreshTime := usage.RefreshAt.Local().Format("2006-01-02 15:04:05")
	fmt.Fprintf(&b, "Refresh date: %s (%s)\n", refreshTime, relativeDuration(now, usage.RefreshAt))
	return b.String()
}

func relativeDuration(now, target time.Time) string {
	d := target.Sub(now)
	if d < 0 {
		d = -d
	}
	minutes := int(d.Round(time.Minute) / time.Minute)
	if minutes < 60 {
		return fmt.Sprintf("%dmin", minutes)
	}
	hours := minutes / 60
	days := hours / 24
	remainingHours := hours % 24
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, remainingHours)
	}
	return fmt.Sprintf("%dh", hours)
}

func clampPercent(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func init() {
	codexUsageCmd.Flags().String("base-url", defaultCodexBaseURL, "Codex usage API base URL")
	rootCmd.AddCommand(codexUsageCmd)
}
