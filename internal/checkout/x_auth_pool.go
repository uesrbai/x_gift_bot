package checkout

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"xgift/internal/vault"
)

const xAuthProfilesKey = "x-auth-profiles"

type XAuthProfile struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	AuthToken     string `json:"auth_token"`
	Ct0           string `json:"ct0"`
	Enabled       bool   `json:"enabled"`
	CooldownUntil int64  `json:"cooldown_until,omitempty"`
	LastUsed      int64  `json:"last_used,omitempty"`
}

var xAuthPoolMu sync.Mutex

func ReadXAuthProfiles(v *vault.Vault) ([]XAuthProfile, error) {
	raw, err := v.Get(xAuthProfilesKey)
	if err == nil {
		defer clear(raw)
		var profiles []XAuthProfile
		if json.Unmarshal(raw, &profiles) != nil {
			return nil, errors.New("X 登录会话池数据损坏")
		}
		return profiles, nil
	}
	if !errors.Is(err, sql.ErrNoRows) && !strings.Contains(err.Error(), "secret "+xAuthProfilesKey+" unavailable") {
		return nil, err
	}
	raw, err = v.Get("cookies")
	if err != nil {
		return nil, err
	}
	defer clear(raw)
	var legacy struct {
		Cookies []struct {
			Name  string `json:"Name"`
			Value string `json:"Value"`
		}
	}
	if json.Unmarshal(raw, &legacy) != nil {
		return nil, errors.New("旧版 X Cookie 数据损坏")
	}
	var authToken, ct0 string
	for _, c := range legacy.Cookies {
		switch c.Name {
		case "auth_token":
			authToken = c.Value
		case "ct0":
			ct0 = c.Value
		}
	}
	if authToken == "" || ct0 == "" {
		return nil, errors.New("X 登录 Cookie 缺少 auth_token 或 ct0")
	}
	return []XAuthProfile{{ID: "legacy", Label: "旧版登录会话", AuthToken: authToken, Ct0: ct0, Enabled: true}}, nil
}

func SaveXAuthProfiles(v *vault.Vault, profiles []XAuthProfile) error {
	b, err := json.Marshal(profiles)
	if err != nil {
		return err
	}
	defer clear(b)
	return v.Put(xAuthProfilesKey, b)
}

func SelectXAuthProfile(v *vault.Vault) (XAuthProfile, error) {
	xAuthPoolMu.Lock()
	defer xAuthPoolMu.Unlock()
	profiles, err := ReadXAuthProfiles(v)
	if err != nil {
		return XAuthProfile{}, err
	}
	now := time.Now().Unix()
	available := make([]int, 0, len(profiles))
	for i := range profiles {
		if profiles[i].Enabled && profiles[i].AuthToken != "" && profiles[i].Ct0 != "" && profiles[i].CooldownUntil <= now {
			available = append(available, i)
		}
	}
	if len(available) == 0 {
		return XAuthProfile{}, errors.New("没有可用的 X 登录会话，请在管理页添加或启用 auth_token + ct0")
	}
	sort.SliceStable(available, func(i, j int) bool {
		a, b := profiles[available[i]], profiles[available[j]]
		if a.LastUsed != b.LastUsed {
			return a.LastUsed < b.LastUsed
		}
		return a.ID < b.ID
	})
	idx := available[0]
	profiles[idx].LastUsed = now
	if profiles[idx].ID != "legacy" {
		if err := SaveXAuthProfiles(v, profiles); err != nil {
			return XAuthProfile{}, err
		}
	}
	return profiles[idx], nil
}

func CooldownXAuthProfile(v *vault.Vault, id string, duration time.Duration) error {
	if id == "" || id == "legacy" {
		return nil
	}
	xAuthPoolMu.Lock()
	defer xAuthPoolMu.Unlock()
	profiles, err := ReadXAuthProfiles(v)
	if err != nil {
		return err
	}
	until := time.Now().Add(duration).Unix()
	changed := false
	for i := range profiles {
		if profiles[i].ID == id {
			if profiles[i].CooldownUntil < until {
				profiles[i].CooldownUntil = until
				changed = true
			}
			break
		}
	}
	if !changed {
		return nil
	}
	return SaveXAuthProfiles(v, profiles)
}

func ValidateXAuthProfile(p XAuthProfile) error {
	p.Label = strings.TrimSpace(p.Label)
	p.AuthToken = strings.TrimSpace(p.AuthToken)
	p.Ct0 = strings.TrimSpace(p.Ct0)
	if p.AuthToken == "" || p.Ct0 == "" {
		return errors.New("auth_token 和 ct0 都不能为空")
	}
	if strings.ContainsAny(p.AuthToken, "
;") || strings.ContainsAny(p.Ct0, "
;") {
		return errors.New("Cookie 值包含非法字符")
	}
	if len(p.AuthToken) > 4096 || len(p.Ct0) > 4096 {
		return errors.New("Cookie 值过长")
	}
	return nil
}

func NewXAuthProfileID() string {
	return fmt.Sprintf("xauth-%d", time.Now().UnixNano())
}
