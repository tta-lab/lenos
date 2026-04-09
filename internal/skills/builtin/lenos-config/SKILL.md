---
name: lenos-config
description: Configure Lenos settings including providers, skills, permissions, and behavior options. Use when the user needs help with config.json configuration, setting up providers, configuring tools, or changing Lenos behavior.
---

# Lenos Configuration

Lenos uses JSON configuration files with the following priority (highest to lowest):

1. `.config.json` (project-local, hidden)
2. `config.json` (project-local)
3. `$XDG_CONFIG_HOME/lenos/config.json` or `$HOME/.config/lenos/config.json` (global)

## Basic Structure

```json
{
  "$schema": "https://github.com/tta-lab/lenos/raw/main/schema.json",
  "options": {}
}
```

The `$schema` property enables IDE autocomplete but is optional.

## Common Configurations

### Project-Local Skills

Add a relative path to keep project-specific skills alongside your code:

```json
{
  "options": {
    "skills_paths": ["./skills"]
  }
}
```

> [!IMPORTANT]
>  Keep in mind that the following paths are loaded by default, so they DO NOT NEED to be added to `skill_paths`:
>
>  * `.agents/skills`
>  * `.lenos/skills`
>  * `.claude/skills`
>  * `.cursor/skills`

### Custom Provider

```json
{
  "providers": {
    "deepseek": {
      "type": "openai-compat",
      "base_url": "https://api.deepseek.com/v1",
      "api_key": "$DEEPSEEK_API_KEY",
      "models": [
        {
          "id": "deepseek-chat",
          "name": "Deepseek V3",
          "context_window": 64000
        }
      ]
    }
  }
}
```

### Tool Permissions

```json
{
  "permissions": {
    "allowed_tools": ["bash", "sourcegraph"]
  }
}
```

### Disable Built-in Tools

```json
{
  "options": {
    "disabled_tools": ["sourcegraph"]
  }
}
```

### Disable Skills

```json
{
  "options": {
    "disabled_skills": ["lenos-config"]
  }
}
```

`disabled_skills` disables skills by name, including both builtin skills and
skills discovered from disk paths.

### Debug Options

```json
{
  "options": {
    "debug": true
  }
}
```

### Attribution Settings

```json
{
  "options": {
    "attribution": {
      "trailer_style": "assisted-by",
      "generated_with": true
    }
  }
}
```

## Environment Variables

- `LENOS_GLOBAL_CONFIG` - Override global config location
- `LENOS_GLOBAL_DATA` - Override data directory location
- `LENOS_SKILLS_DIR` - Override default skills directory

## Provider Types

- `openai` - For OpenAI or OpenAI-compatible APIs that route through OpenAI
- `openai-compat` - For non-OpenAI providers with OpenAI-compatible APIs
- `anthropic` - For Anthropic-compatible APIs
