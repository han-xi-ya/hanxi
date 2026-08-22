package wechat

import (
	"context"
	"fmt"
	"time"
)

type qrCodeResp struct {
	QRCode           string `json:"qrcode"`
	QRCodeImgContent string `json:"qrcode_img_content"`
	Ret              int    `json:"ret"`
	ErrMsg           string `json:"errmsg"`
}

type qrStatusResp struct {
	Status      string `json:"status"`
	BotToken    string `json:"bot_token"`
	IlinkBotID  string `json:"ilink_bot_id"`
	IlinkUserID string `json:"ilink_user_id"`
	BaseURL     string `json:"baseurl"`
	Ret         int    `json:"ret"`
	ErrMsg      string `json:"errmsg"`
}

// FetchLoginQRCode 获取微信登录二维码
func (c *Client) FetchLoginQRCode(ctx context.Context) (*QRInfo, error) {
	reqBody := map[string]any{
		"local_token_list": []string{},
	}
	var resp qrCodeResp
	err := c.post(ctx, "/ilink/bot/get_bot_qrcode?bot_type=3", "", reqBody, &resp)
	if err != nil {
		return nil, err
	}
	if resp.Ret != 0 && resp.Ret != 200 {
		return nil, fmt.Errorf("fetch qrcode failed: ret=%d, msg=%s", resp.Ret, resp.ErrMsg)
	}
	if resp.QRCode == "" {
		return nil, fmt.Errorf("empty qrcode returned by server")
	}

	return &QRInfo{
		QRCode:    resp.QRCode,
		QRCodeURL: resp.QRCodeImgContent,
	}, nil
}

// PollQRStatus 查询指定二维码的当前状态（单次长轮询，推荐超时 35s）
func (c *Client) PollQRStatus(ctx context.Context, qrcode string) (*QRStatus, error) {
	endpoint := fmt.Sprintf("/ilink/bot/get_qrcode_status?qrcode=%s", qrcode)

	// 单次请求设置 35 秒超时
	pollCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()

	var resp qrStatusResp
	err := c.get(pollCtx, endpoint, "", &resp)
	if err != nil {
		return nil, err
	}

	return &QRStatus{
		Status:      resp.Status,
		BotToken:    resp.BotToken,
		IlinkBotID:  resp.IlinkBotID,
		IlinkUserID: resp.IlinkUserID,
		BaseURL:     resp.BaseURL,
		Message:     resp.ErrMsg,
	}, nil
}
