package site

import (
	"strings"
	"time"

	"xgift/internal/checkout"
)

type xAuthAdminView struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	AuthTokenHint string `json:"auth_token_hint"`
	Ct0Hint       string `json:"ct0_hint"`
	Enabled       bool   `json:"enabled"`
	CooldownUntil int64  `json:"cooldown_until,omitempty"`
	LastUsed      int64  `json:"last_used,omitempty"`
}

func maskSecret(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 8 {
		return "••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

func (s *server) xAuthProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := checkout.ReadXAuthProfiles(s.vault)
	if err != nil {
		message(w, 503, "读取 X 登录会话失败："+err.Error())
		return
	}
	now := time.Now().Unix()
	available, cooling := 0, 0
	out := make([]xAuthAdminView, 0, len(profiles))
	for _, p := range profiles {
		if p.Enabled && p.CooldownUntil <= now {
			available++
		}
		if p.CooldownUntil > now {
			cooling++
		}
		out = append(out, xAuthAdminView{
			ID: p.ID, Label: p.Label,
			AuthTokenHint: maskSecret(p.AuthToken),
			Ct0Hint: maskSecret(p.Ct0),
			Enabled: p.Enabled, CooldownUntil: p.CooldownUntil, LastUsed: p.LastUsed,
		})
	}
	reply(w, 200, map[string]any{"profiles": out, "count": len(out), "available": available, "cooling": cooling})
}

func (s *server) addXAuthProfile(w http.ResponseWriter, r *http.Request) {
	var p checkout.XAuthProfile
	if !decode(w, r, &p) {
		return
	}
	if err := checkout.ValidateXAuthProfile(p); err != nil {
		message(w, 400, err.Error())
		return
	}
	p.ID = checkout.NewXAuthProfileID()
	p.Label = strings.TrimSpace(p.Label)
	if p.Label == "" {
		p.Label = "X 登录会话"
	}
	p.Enabled = true
	p.CooldownUntil = 0
	p.LastUsed = 0
	profiles, err := checkout.ReadXAuthProfiles(s.vault)
	if err != nil {
		message(w, 503, "读取 X 登录会话失败："+err.Error())
		return
	}
	profiles = append(profiles, p)
	if err = checkout.SaveXAuthProfiles(s.vault, profiles); err != nil {
		message(w, 503, "保存 X 登录会话失败："+err.Error())
		return
	}
	reply(w, 200, map[string]any{"ok": true, "id": p.ID, "label": p.Label})
}

func (s *server) toggleXAuthProfile(w http.ResponseWriter, r *http.Request) {
	var q struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if !decode(w, r, &q) {
		return
	}
	profiles, err := checkout.ReadXAuthProfiles(s.vault)
	if err != nil {
		message(w, 503, "读取 X 登录会话失败："+err.Error())
		return
	}
	found := false
	for i := range profiles {
		if profiles[i].ID == strings.TrimSpace(q.ID) {
			profiles[i].Enabled = q.Enabled
			found = true
			break
		}
	}
	if !found {
		message(w, 404, "X 登录会话不存在。")
		return
	}
	if err = checkout.SaveXAuthProfiles(s.vault, profiles); err != nil {
		message(w, 503, "更新 X 登录会话失败："+err.Error())
		return
	}
	reply(w, 200, map[string]any{"ok": true})
}

func (s *server) removeXAuthProfile(w http.ResponseWriter, r *http.Request) {
	var q struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &q) {
		return
	}
	profiles, err := checkout.ReadXAuthProfiles(s.vault)
	if err != nil {
		message(w, 503, "读取 X 登录会话失败："+err.Error())
		return
	}
	kept := make([]checkout.XAuthProfile, 0, len(profiles))
	found := false
	for _, p := range profiles {
		if p.ID == strings.TrimSpace(q.ID) {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	if !found {
		message(w, 404, "X 登录会话不存在。")
		return
	}
	if len(kept) == 0 {
		message(w, 400, "至少保留一组 X 登录会话；如需更换，请先添加新的会话。")
		return
	}
	if err = checkout.SaveXAuthProfiles(s.vault, kept); err != nil {
		message(w, 503, "删除 X 登录会话失败："+err.Error())
		return
	}
	reply(w, 200, map[string]any{"ok": true})
}

func (s *server) rotateXAuthProfiles(w http.ResponseWriter, r *http.Request) {
	profile, err := checkout.SelectXAuthProfile(s.vault)
	if err != nil {
		message(w, 503, err.Error())
		return
	}
	reply(w, 200, map[string]any{"ok": true, "id": profile.ID, "label": profile.Label})
}
