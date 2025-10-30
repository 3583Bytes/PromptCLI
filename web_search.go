package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

type searchResult struct {
	title, link, snippet string
}

func performWebSearch(query string, logger *Logger) (string, error) {
	logger.Log(fmt.Sprintf("performWebSearch query: %s", query))

	// 1. Construct the search URL
	encodedQuery := url.QueryEscape(query)
	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", encodedQuery)

	// 2. Make the HTTP request
	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create search request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/109.0.0.0 Safari/537.36")

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to perform search request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return "", fmt.Errorf("search request failed with status code: %d", res.StatusCode)
	}

	// 3. Parse the HTML response
	doc, err := html.Parse(res.Body)
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML response: %w", err)
	}

	// 4. Find and extract search results
	results := findResults(doc)

	// 5. Format the results
	if len(results) == 0 {
		return "No results found.", nil
	}

	var summary strings.Builder
	summary.WriteString("Search results:\n")
	for i, result := range results {
		if i > 4 { // Limit to 5 results
			break
		}
		summary.WriteString(fmt.Sprintf("%d. %s - %s\n", i+1, result.title, result.link))
		if result.snippet != "" {
			summary.WriteString(fmt.Sprintf("   %s\n", result.snippet))
		}
	}

	return summary.String(), nil
}

// findResults traverses the HTML node tree and extracts search results.
func findResults(n *html.Node) []searchResult {
	var results []searchResult
	if n.Type == html.ElementNode && n.Data == "div" {
		if hasClass(n, "result") {
			var result searchResult
			result.title, result.link = extractTitleAndLink(n)
			result.snippet = extractSnippet(n)
			if result.title != "" && result.link != "" {
				results = append(results, result)
			}
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		results = append(results, findResults(c)...)
	}
	return results
}

func extractTitleAndLink(n *html.Node) (string, string) {
	var title, link string
	var crawler func(*html.Node)
	crawler = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" && hasClass(node, "result__a") {
			title = getText(node)
			link = getAttr(node, "href")
			// Clean up the link
			if strings.HasPrefix(link, "/l/") {
				parsedURL, err := url.Parse("https:" + link)
				if err == nil {
					link = parsedURL.Query().Get("uddg")
				}
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			crawler(c)
		}
	}
	crawler(n)
	return title, link
}

func extractSnippet(n *html.Node) string {
	var snippet string
	var crawler func(*html.Node)
	crawler = func(node *html.Node) {
		if node.Type == html.ElementNode && hasClass(node, "result__snippet") {
			snippet = getText(node)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			crawler(c)
		}
	}
	crawler(n)
	return snippet
}

// hasClass checks if a node has a specific CSS class.
func hasClass(n *html.Node, className string) bool {
	for _, a := range n.Attr {
		if a.Key == "class" {
			classes := strings.Fields(a.Val)
			for _, c := range classes {
				if c == className {
					return true
				}
			}
		}
	}
	return false
}

// getAttr gets the value of an attribute from a node.
func getAttr(n *html.Node, attrName string) string {
	for _, a := range n.Attr {
		if a.Key == attrName {
			return a.Val
		}
	}
	return ""
}

// getText recursively gets the text content of a node.
func getText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var text strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		text.WriteString(getText(c))
	}
	return strings.TrimSpace(text.String())
}
