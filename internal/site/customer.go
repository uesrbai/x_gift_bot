package site

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"context"
	"time"
	"xgift/internal/checkout"
)

// Customer lookup is independent of folder filters and pagination.
func (s *server) customerOrder(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	user, validUser := checkout.NormalizeUsername(r.URL.Query().Get("username"))
	var c codeRow
	var digest string
	query := "SELECT id,hint,batch,months,status,username,message,created,updated,progress,COALESCE(recipient_id,''),copyable,hash FROM codes WHERE "
	var arg string
	if id != "" {
		if !folderIDPattern.MatchString(id) {
			message(w, 400, "订单编号无效。")
			return
		}
		query += "id=?"
		arg = id
	} else {
		if !validUser {
			message(w, 400, "请输入正确的客户 X 用户名。")
			return
		}
		query += "username=?"
		arg = user
	}
	err := s.db.QueryRow(query, arg).Scan(&c.ID, &c.Hint, &c.Batch, &c.Months, &c.Status, &c.Username, &c.Message, &c.Created, &c.Updated, &c.Progress, &c.RecipientID, &c.Copyable, &digest)
	if errors.Is(err, sql.ErrNoRows) {
		if id != "" { message(w, 404, "未找到该兑换码订单。"); return }
		// Manual/public checkouts are encrypted in Vault and are not rows in
		// site.db's codes table. Keep this read-only: never create an order.
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		recipient, lookupErr := checkout.LookupRecipient(ctx, s.vault, user, s.port)
		if lookupErr != nil {
			message(w, 503, "未找到兑换码订单，但 X 账号标识查询失败，暂时无法排除独立付款订单。请稍后重试。")
			return
		}
		records := make([]map[string]any, 0, 3)
		for _, candidate := range []struct{ key, source string }{
			{"checkout:" + recipient, "独立付款订单（账号 ID）"},
			{"checkout:" + user, "旧版付款订单（用户名）"},
			{"public-checkout:" + recipient, "公开付款订单"},
		} {
			raw, readErr := s.vault.Get(candidate.key)
			if errors.Is(readErr, sql.ErrNoRows) { continue }
			if readErr != nil {
				message(w, 503, "加密订单记录读取失败，无法确认付款状态；请检查 Vault。")
				return
			}
			var order checkout.Record
			if candidate.source == "公开付款订单" {
				var wrapped struct { Order checkout.Record `json:"order"` }
				readErr = json.Unmarshal(raw, &wrapped)
				order = wrapped.Order
			} else {
				readErr = json.Unmarshal(raw, &order)
			}
			clear(raw)
			if readErr != nil || order.Username != user || order.RecipientID != recipient {
				message(w, 409, "发现无法与当前账号一致性校验的付款记录；请人工核实，记录未修改。")
				return
			}
			records = append(records, map[string]any{
				"source": candidate.source, "status": order.Status, "months": order.Months,
				"created": order.Created, "submitted": order.SubmittedAt != 0,
				"link_blocked": order.LinkBlocked, "requires_review": order.Status != "succeeded",
			})
		}
		reply(w, 200, map[string]any{"order": nil, "code": "", "checkout_url": "", "previous_checkout_url": "",
			"can_recover": false, "replacement_count": 0, "vault_orders": records,
			"notice": "兑换码订单表没有该客户记录。独立付款记录已单独查询；这不代表可以跳过付款安全检查。"})
		return
	}
	if err != nil {
		message(w, 503, "无法读取客户订单。")
		return
	}
	s.reconcileStatus(r.Context(), &c)
	plain := ""
	if c.Copyable {
		b, e := s.vault.Get("redemption:" + c.ID)
		if e != nil {
			message(w, 503, "无法读取卡密，请稍后重试。")
			return
		}
		defer clear(b)
		if !codePattern.Match(b) || hash(string(b)) != digest {
			message(w, 503, "卡密校验未通过，请核实加密记录。")
			return
		}
		plain = string(b)
	}
	var order checkout.Record
	link := ""
	previousLink := ""
	if c.RecipientID != "" {
		b, e := s.vault.Get("checkout:" + c.RecipientID)
		if e == nil {
			defer clear(b)
			if json.Unmarshal(b, &order) == nil && order.RecipientID == c.RecipientID && order.Username == c.Username && order.Months == c.Months {
				if c.Status != "succeeded" {
					link = checkout.CheckoutLink(&order)
				}

			}
		}
	}
	reply(w, 200, map[string]any{"order": c, "code": plain, "checkout_url": link, "previous_checkout_url": previousLink, "can_recover": c.Status == "review" && c.RecipientID != "", "replacement_count": order.ReplacementCount})
}
