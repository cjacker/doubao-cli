package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/c-bata/go-prompt"
)

// 定义请求体结构（流式请求支持）
type DoubaoRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Stream      bool      `json:"stream,omitempty"` // 开启流式输出
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// 流式响应结构
type StreamResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Choices []StreamChoice `json:"choices"`
	Error   *Error         `json:"error,omitempty"`
}

type StreamChoice struct {
	Delta        Message `json:"delta"`
	FinishReason string  `json:"finish_reason"`
	Index        int     `json:"index"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// 全局配置
type Config struct {
	APIKey     string
	EndpointID string
	Region     string
	Timeout    int
}

var (
	conversationHistory []Message
	config              Config
)

// 动态加载动画（新增停止信号，支持立即终止）
func loadingAnimation(stop chan bool, done chan bool, wg *sync.WaitGroup) {
	defer wg.Done()
	fmt.Print("\n豆包：正在思考")
	chars := []string{".", "..", "...", "...."}
	idx := 0

	for {
		select {
		case <-stop:
			// 立即停止动画，清除加载提示，准备输出回答
			fmt.Print("\r豆包：") // 只保留"豆包："前缀，清除加载文字
			done <- true
			return
		default:
			fmt.Printf("\r豆包：正在思考%s", chars[idx%4])
			idx++
			time.Sleep(500 * time.Millisecond)
		}
	}
}

// 发送流式请求（核心优化：第一个字符输出时立即停止加载动画）
func sendStreamRequest(messages []Message, stopChan chan bool, doneChan chan bool) (string, error) {
	baseURL := fmt.Sprintf("https://ark.%s.volces.com/api/v3/chat/completions", config.Region)

	requestBody := DoubaoRequest{
		Model:       config.EndpointID,
		MaxTokens:   2000,
		Temperature: 0.7,
		Stream:      true,
		Messages:    messages,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("构造请求体失败：%v", err)
	}

	req, err := http.NewRequest("POST", baseURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("创建请求失败：%v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+config.APIKey)

	// 优化HTTP客户端
	client := &http.Client{
		Timeout: time.Duration(config.Timeout) * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives:     false,
			IdleConnTimeout:       30 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		errMsg := fmt.Errorf("发送请求失败：%v", err)
		if os.IsTimeout(err) {
			errMsg = fmt.Errorf("%v\n提示：API响应过慢，建议：1.延长超时时间（--timeout 180） 2.检查网络 3.稍后重试", errMsg)
		}
		return "", errMsg
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("请求失败，状态码：%d", resp.StatusCode)
	}

	// 流式读取响应
	scanner := bufio.NewScanner(resp.Body)
	scanner.Split(bufio.ScanLines)

	var fullContent string
	firstContent := true // 标记是否是第一个输出字符

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, ": ") {
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			line = strings.TrimPrefix(line, "data: ")
			if line == "[DONE]" {
				break
			}

			var streamResp StreamResponse
			err := json.Unmarshal([]byte(line), &streamResp)
			if err != nil {
				continue
			}

			content := streamResp.Choices[0].Delta.Content
			if content != "" {
				// 第一个字符输出前，立即停止加载动画
				if firstContent {
					stopChan <- true     // 发送停止信号
					<-doneChan           // 等待动画完全停止
					firstContent = false // 标记已输出第一个字符
				}
				fmt.Print(content) // 实时输出回答字符
				fullContent += content
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("读取流式响应失败：%v", err)
	}

	if fullContent == "" {
		return "", fmt.Errorf("未获取到有效回答")
	}

	return fullContent, nil
}

// 输入处理函数
func executor(in string) {
	in = strings.TrimSpace(in)

	// 退出逻辑
	if in == "q" || in == "quit" {
		fmt.Println("\n豆包：感谢使用，再见！")
		os.Exit(0)
	}

	// 清空上下文
	if in == "clear" {
		conversationHistory = []Message{}
		fmt.Println("豆包：已清空所有对话上下文！")
		return
	}

	// 空输入校验
	if in == "" {
		fmt.Println("豆包：问题不能为空，请重新输入！")
		return
	}

	// 追加用户消息
	conversationHistory = append(conversationHistory, Message{Role: "user", Content: in})

	// 初始化动画控制通道
	stopChan := make(chan bool) // 停止动画信号
	doneChan := make(chan bool) // 动画完成停止信号
	var wg sync.WaitGroup
	wg.Add(1)

	// 启动加载动画
	go loadingAnimation(stopChan, doneChan, &wg)

	// 发送流式请求（传入动画控制通道）
	fullContent, err := sendStreamRequest(conversationHistory, stopChan, doneChan)

	// 确保动画完全停止（无论成功/失败）
	select {
	case stopChan <- true:
		<-doneChan
	default:
		// 动画已提前停止，无需重复发送
	}
	wg.Wait()
	close(stopChan)
	close(doneChan)

	// 处理错误
	if err != nil {
		fmt.Printf("[错误] %v\n\n", err)
		conversationHistory = conversationHistory[:len(conversationHistory)-1]
		return
	}

	// 追加完整回答到上下文
	conversationHistory = append(conversationHistory, Message{Role: "assistant", Content: fullContent})
	fmt.Printf("\n\n") // 回答结束后换行
}

func completer(in prompt.Document) []prompt.Suggest {
	return []prompt.Suggest{}
}

func main() {
	// 解析命令行参数
	apiKey := flag.String("apikey", "", "火山方舟API Key（必填）")
	endpointID := flag.String("endpoint", "", "火山方舟Endpoint ID（必填）")
	region := flag.String("region", "cn-beijing", "火山方舟地域（可选）")
	timeout := flag.Int("timeout", 120, "请求超时时间（秒，建议设为120-180）")
	flag.Parse()

	config = Config{
		APIKey:     *apiKey,
		EndpointID: *endpointID,
		Region:     *region,
		Timeout:    *timeout,
	}

	if config.APIKey == "" || config.EndpointID == "" {
		fmt.Println("错误：必须指定 --apikey 和 --endpoint 参数！")
		fmt.Println("使用示例：go run main.go --apikey sk-xxxxxx --endpoint ep-xxxxxx --timeout 180")
		os.Exit(1)
	}

	// 欢迎信息
	fmt.Println("==================== 豆包多轮对话CLI ====================")
	fmt.Println("说明：1.输入 q/quit 退出对话；输入 clear 清空上下文")
	fmt.Println("=========================================================")

	prompt.New(executor, completer).Run()
}
