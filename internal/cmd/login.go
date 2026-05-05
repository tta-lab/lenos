package cmd

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"os/signal"

	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"
	"github.com/pkg/browser"
	"github.com/spf13/cobra"
	hyperp "github.com/tta-lab/lenos/internal/agent/hyper"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/oauth"
	"github.com/tta-lab/lenos/internal/oauth/codex"
	"github.com/tta-lab/lenos/internal/oauth/copilot"
	"github.com/tta-lab/lenos/internal/oauth/hyper"
	"github.com/tta-lab/lenos/internal/workspace"
)

var loginCmd = &cobra.Command{
	Aliases: []string{"auth"},
	Use:     "login [platform]",
	Short:   "Login Lenos to a platform",
	Long: `Login Lenos to a specified platform.
The platform should be provided as an argument.
Available platforms are: hyper, copilot, codex.`,
	Example: `
# Authenticate with Hyper
lenos login

# Authenticate with GitHub Copilot
lenos login copilot

# Authenticate with Codex (ChatGPT)
lenos login codex
  `,
	ValidArgs: []cobra.Completion{
		"hyper",
		"copilot",
		"github",
		"github-copilot",
		"codex",
		"chatgpt",
	},
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ws, cleanup, err := setupWorkspace(cmd, "", nil, false)
		if err != nil {
			return err
		}
		defer cleanup()

		progressEnabled := ws.Config().Options.Progress == nil || *ws.Config().Options.Progress
		if progressEnabled && supportsProgressBar() {
			_, _ = fmt.Fprintf(os.Stderr, ansi.SetIndeterminateProgressBar)
			defer func() { _, _ = fmt.Fprintf(os.Stderr, ansi.ResetProgressBar) }()
		}

		provider := "hyper"
		if len(args) > 0 {
			provider = args[0]
		}
		switch provider {
		case "hyper":
			return loginHyper(ws)
		case "copilot", "github", "github-copilot":
			return loginCopilot(cmd.Context(), ws)
		case "codex", "chatgpt":
			return loginCodex(cmd.Context(), ws)
		default:
			return fmt.Errorf("unknown platform: %s", args[0])
		}
	},
}

func loginHyper(ws workspace.Workspace) error {
	if !hyperp.Enabled() {
		return fmt.Errorf("hyper not enabled")
	}
	ctx := getLoginContext()

	resp, err := hyper.InitiateDeviceAuth(ctx)
	if err != nil {
		return err
	}

	if clipboard.WriteAll(resp.UserCode) == nil {
		fmt.Println("The following code should be on clipboard already:")
	} else {
		fmt.Println("Copy the following code:")
	}

	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Bold(true).Render(resp.UserCode))
	fmt.Println()
	fmt.Println("Press enter to open this URL, and then paste it there:")
	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Hyperlink(resp.VerificationURL, "id=hyper").Render(resp.VerificationURL))
	fmt.Println()
	waitEnter()
	if err := browser.OpenURL(resp.VerificationURL); err != nil {
		fmt.Println("Could not open the URL. You'll need to manually open the URL in your browser.")
	}

	fmt.Println("Exchanging authorization code...")
	refreshToken, err := hyper.PollForToken(ctx, resp.DeviceCode, resp.ExpiresIn)
	if err != nil {
		return err
	}

	fmt.Println("Exchanging refresh token for access token...")
	token, err := hyper.ExchangeToken(ctx, refreshToken)
	if err != nil {
		return err
	}

	fmt.Println("Verifying access token...")
	introspect, err := hyper.IntrospectToken(ctx, token.AccessToken)
	if err != nil {
		return fmt.Errorf("token introspection failed: %w", err)
	}
	if !introspect.Active {
		return fmt.Errorf("access token is not active")
	}

	if err := cmp.Or(
		ws.SetConfigField(config.ScopeGlobal, "providers.hyper.api_key", token.AccessToken),
		ws.SetConfigField(config.ScopeGlobal, "providers.hyper.oauth", token),
	); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("You're now authenticated with Hyper!")
	return nil
}

func loginCopilot(ctx context.Context, ws workspace.Workspace) error {
	loginCtx := getLoginContext()

	cfg := ws.Config()
	if pc, ok := cfg.Providers.Get("copilot"); ok && pc.OAuthToken != nil {
		fmt.Println("You are already logged in to GitHub Copilot.")
		return nil
	}

	diskToken, hasDiskToken := copilot.RefreshTokenFromDisk()
	var token *oauth.Token

	switch {
	case hasDiskToken:
		fmt.Println("Found existing GitHub Copilot token on disk. Using it to authenticate...")

		t, err := copilot.RefreshToken(loginCtx, diskToken)
		if err != nil {
			return fmt.Errorf("unable to refresh token from disk: %w", err)
		}
		token = t
	default:
		fmt.Println("Requesting device code from GitHub...")
		dc, err := copilot.RequestDeviceCode(loginCtx)
		if err != nil {
			return err
		}

		fmt.Println()
		fmt.Println("Open the following URL and follow the instructions to authenticate with GitHub Copilot:")
		fmt.Println()
		fmt.Println(lipgloss.NewStyle().Hyperlink(dc.VerificationURI, "id=copilot").Render(dc.VerificationURI))
		fmt.Println()
		fmt.Println("Code:", lipgloss.NewStyle().Bold(true).Render(dc.UserCode))
		fmt.Println()
		fmt.Println("Waiting for authorization...")

		t, err := copilot.PollForToken(loginCtx, dc)
		if err == copilot.ErrNotAvailable {
			fmt.Println()
			fmt.Println("GitHub Copilot is unavailable for this account. To signup, go to the following page:")
			fmt.Println()
			fmt.Println(lipgloss.NewStyle().Hyperlink(copilot.SignupURL, "id=copilot-signup").Render(copilot.SignupURL))
			fmt.Println()
			fmt.Println("You may be able to request free access if eligible. For more information, see:")
			fmt.Println()
			fmt.Println(lipgloss.NewStyle().Hyperlink(copilot.FreeURL, "id=copilot-free").Render(copilot.FreeURL))
		}
		if err != nil {
			return err
		}
		token = t
	}

	if err := cmp.Or(
		ws.SetConfigField(config.ScopeGlobal, "providers.copilot.api_key", token.AccessToken),
		ws.SetConfigField(config.ScopeGlobal, "providers.copilot.oauth", token),
	); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("You're now authenticated with GitHub Copilot!")
	return nil
}

func loginCodex(ctx context.Context, ws workspace.Workspace) error {
	loginCtx := getLoginContext()

	cfg := ws.Config()
	if pc, ok := cfg.Providers.Get("codex"); ok && pc.OAuthToken != nil {
		fmt.Println("You are already logged in to Codex (ChatGPT).")
		return nil
	}

	fmt.Println("Requesting device code from OpenAI...")
	dar, err := codex.InitiateDeviceAuth(loginCtx)
	if err != nil {
		return fmt.Errorf("device auth init: %w", err)
	}

	if clipboard.WriteAll(dar.UserCode) == nil {
		fmt.Println("The following code should be on clipboard already:")
	} else {
		fmt.Println("Copy the following code:")
	}
	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Bold(true).Render(dar.UserCode))
	fmt.Println()
	fmt.Println("Press enter to open this URL, and then paste it there:")
	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Hyperlink(dar.VerifyURL, "id=codex").Render(dar.VerifyURL))
	fmt.Println()
	waitEnter()
	if err := browser.OpenURL(dar.VerifyURL); err != nil {
		fmt.Println("Could not open the URL. Please open it manually.")
	}

	fmt.Println("Waiting for authorization...")
	tokens, err := codex.PollForToken(loginCtx, dar)
	if err != nil {
		return fmt.Errorf("poll for token: %w", err)
	}

	accountID := codex.ExtractChatGPTAccountID(tokens.IDToken)
	if accountID == "" {
		// Fall back to access_token (matches forgecode behavior)
		accountID = codex.ExtractChatGPTAccountID(tokens.AccessToken)
	}
	if accountID == "" {
		return fmt.Errorf("could not extract chatgpt_account_id from tokens")
	}

	if err := cmp.Or(
		ws.SetConfigField(config.ScopeGlobal, "providers.codex.api_key", tokens.AccessToken),
		ws.SetConfigField(config.ScopeGlobal, "providers.codex.oauth", tokens.Token),
		ws.SetConfigField(config.ScopeGlobal, "providers.codex.extra_headers.ChatGPT-Account-Id", accountID),
		ws.SetConfigField(config.ScopeGlobal, "providers.codex.extra_headers.originator", "codex_cli_rs"),
		ws.SetConfigField(config.ScopeGlobal, "providers.codex.extra_headers.OpenAI-Beta", "responses=experimental"),
	); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("You're now authenticated with Codex (ChatGPT)!")
	return nil
}

func getLoginContext() context.Context {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	go func() {
		<-ctx.Done()
		cancel()
		os.Exit(1)
	}()
	return ctx
}

func waitEnter() {
	_, _ = fmt.Scanln()
}
