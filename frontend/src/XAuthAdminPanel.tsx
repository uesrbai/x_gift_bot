import { useEffect, useState } from "react";
import { Alert, Box, Button, Chip, Collapse, Paper, Stack, Switch, TextField, Typography } from "@mui/material";
import ManageAccountsRounded from "@mui/icons-material/ManageAccountsRounded";
import AddRounded from "@mui/icons-material/AddRounded";
import AutorenewRounded from "@mui/icons-material/AutorenewRounded";
import DeleteOutlineRounded from "@mui/icons-material/DeleteOutlineRounded";
import { adminApi as api } from "./adminApi";

type Profile = {
  id: string;
  label: string;
  auth_token_hint: string;
  ct0_hint: string;
  enabled: boolean;
  cooldown_until?: number;
  last_used?: number;
};
type Response = { profiles: Profile[]; count: number; available: number; cooling: number };

export function XAuthAdminPanel() {
  const [data, setData] = useState<Response | null>(null);
  const [form, setForm] = useState({ label: "", auth_token: "", ct0: "" });
  const [open, setOpen] = useState(true);
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState("");

  async function load() {
    try {
      setData(await api<Response>("/api/admin/x-auth"));
    } catch (e) { setMessage((e as Error).message); }
  }
  useEffect(() => { void load(); }, []);

  async function add() {
    setBusy("add"); setMessage("");
    try {
      await api("/api/admin/x-auth", form);
      setForm({label:"",auth_token:"",ct0:""});
      setMessage("X 登录会话已加密保存，并加入自动轮换池。");
      await load();
    } catch (e) { setMessage((e as Error).message); } finally { setBusy(""); }
  }

  async function toggle(p: Profile) {
    setBusy(p.id); setMessage("");
    try {
      await api("/api/admin/x-auth/toggle", {id:p.id, enabled:!p.enabled});
      await load();
    } catch (e) { setMessage((e as Error).message); } finally { setBusy(""); }
  }

  async function remove(p: Profile) {
    if (!window.confirm("确定删除这组 X 登录会话？删除后不会再参与轮换。")) return;
    setBusy("remove:"+p.id); setMessage("");
    try {
      await api("/api/admin/x-auth/remove", {id:p.id});
      setMessage("X 登录会话已删除。");
      await load();
    } catch (e) { setMessage((e as Error).message); } finally { setBusy(""); }
  }

  async function rotate() {
    setBusy("rotate"); setMessage("");
    try {
      const r = await api<{label:string}>("/api/admin/x-auth/rotate", {});
      setMessage(`已切换轮换指针：${r.label}。后续 X 请求会优先使用该会话池中的下一组可用登录。`);
      await load();
    } catch (e) { setMessage((e as Error).message); } finally { setBusy(""); }
  }

  return <Paper variant="outlined" sx={{p:{xs:2.5,sm:3},mb:3}}>
    <Stack direction="row" justifyContent="space-between" alignItems="center">
      <Stack direction="row" spacing={1.5} alignItems="center">
        <ManageAccountsRounded color="primary"/>
        <Box>
          <Typography variant="h2">X 登录 Cookie 管理</Typography>
          <Typography variant="body2" color="text.secondary">auth_token + ct0 加密保存；多组会话自动轮换，异常会话自动冷却。</Typography>
        </Box>
      </Stack>
      <Button onClick={()=>setOpen(v=>!v)}>{open?"收起":"展开"}</Button>
    </Stack>
    <Collapse in={open}>
      <Stack spacing={2.5} sx={{mt:2.5}}>
        {message && <Alert severity="info" onClose={()=>setMessage("")}>{message}</Alert>}
        <Stack direction={{xs:"column",sm:"row"}} spacing={1}>
          <Chip label={`会话 ${data?.count??0}`}/>
          <Chip color="success" variant="outlined" label={`可用 ${data?.available??0}`}/>
          <Chip color="warning" variant="outlined" label={`冷却 ${data?.cooling??0}`}/>
          <Button variant="outlined" startIcon={<AutorenewRounded/>} disabled={!!busy} onClick={()=>void rotate()}>{busy==="rotate"?"切换中…":"立即轮换"}</Button>
        </Stack>

        {!!data?.profiles.length && <Stack spacing={1}>{data.profiles.map(p=>
          <Alert key={p.id} severity={p.enabled ? (p.cooldown_until && p.cooldown_until > Date.now()/1000 ? "warning":"success") : "info"} icon={false}
            action={<Stack direction="row" alignItems="center"><Switch checked={p.enabled} onChange={()=>void toggle(p)} disabled={!!busy}/><Button color="error" size="small" startIcon={<DeleteOutlineRounded/>} onClick={()=>void remove(p)} disabled={!!busy}>删除</Button></Stack>}>
            <b>{p.label}</b> · auth_token {p.auth_token_hint} · ct0 {p.ct0_hint}
            {p.cooldown_until && p.cooldown_until > Date.now()/1000 ? " · 冷却中" : p.enabled ? " · 可用" : " · 已停用"}
          </Alert>
        )}</Stack>}

        <Typography variant="h3">添加登录会话</Typography>
        <TextField label="备注名称" value={form.label} onChange={e=>setForm(v=>({...v,label:e.target.value}))} placeholder="例如：X 主账号-会话1" fullWidth/>
        <TextField label="auth_token" value={form.auth_token} onChange={e=>setForm(v=>({...v,auth_token:e.target.value}))} type="password" autoComplete="off" fullWidth/>
        <TextField label="ct0" value={form.ct0} onChange={e=>setForm(v=>({...v,ct0:e.target.value}))} type="password" autoComplete="off" fullWidth/>
        <Button variant="contained" startIcon={<AddRounded/>} disabled={!!busy || !form.auth_token || !form.ct0} onClick={()=>void add()}>{busy==="add"?"保存中…":"保存并加入轮换池"}</Button>
        <Alert severity="warning">完整 Cookie 只用于本次保存并写入加密 Vault；管理页列表不会回显完整 auth_token / ct0。不要把 Cookie 粘贴到 GitHub、日志或聊天中。</Alert>
      </Stack>
    </Collapse>
  </Paper>;
}
