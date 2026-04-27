//go:build tools

package tools

// This file ensures all project dependencies are retained in go.mod/go.sum
// even before implementation code imports them. Remove after Wave 1+ is merged.

import (
	_ "github.com/cenkalti/backoff/v4"
	_ "github.com/charmbracelet/bubbles"
	_ "github.com/charmbracelet/bubbletea"
	_ "github.com/charmbracelet/lipgloss"
	_ "github.com/sahilm/fuzzy"
	_ "github.com/spf13/viper"
	_ "github.com/stretchr/testify"

	_ "k8s.io/api/apps/v1"
	_ "k8s.io/api/core/v1"
	_ "k8s.io/apimachinery/pkg/apis/meta/v1"
	_ "k8s.io/client-go/kubernetes"

	_ "github.com/anthropics/anthropic-sdk-go"
	_ "github.com/openai/openai-go"
	_ "google.golang.org/genai"
)
