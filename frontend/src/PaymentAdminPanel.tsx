import { useEffect, useState } from "react";
import {
  Alert, Box, Button, Chip, Collapse, Divider, MenuItem, Paper,
  Select, Stack, TextField, Typography, FormControl, InputLabel
} from "@mui/material";
import NetworkCheckRounded from "@mui/icons-material/NetworkCheckRounded";
import CreditCardRounded from "@mui/icons-material/CreditCardRounded";
import AddCardRounded from "@mui/icons-material/AddCardRounded";
import SaveRounded from "@mui/icons-material/SaveRounded";
import DeleteOutlineRounded from "@mui/icons-material/DeleteOutlineRounded";
import LockOpenRounded from "@mui/icons-material/LockOpenRounded";
import { adminApi as api } from "./adminApi";

type NodesResponse = { mode: string; nodes: { id: string; type: string; egress_ip?: string }[]; count: number; available: number; cooling: number };
type Probe = { node: string; type: string; healthy: boolean; stripe_http: number; egress_ip?: string; error?: string };
type CardsResponse = { cards: { last4: string; usable: boolean; problem?: string; blocked?: string; cooling_seconds?: number; pair_cooling?: number }[]; rotation: Record<string, unknown> };
type BillingTemplate = { id: string; name: string; country: string; postal: string; line1: string; line2?: string; city: string; state: string };

export function PaymentAdminPanel() {
  const [nodes, setNodes] = useState<NodesResponse | null>(null);
  const [cards, setCards] = useState<CardsResponse | null>(null);
  const [templates, setTemplates] = useState<BillingTemplate[]>([]);
  const [nodeJSON, setNodeJSON] = useState("");
  const [probe, setProbe] = useState<Probe[]>([]);
  const [openNodes, setOpenNodes] = useState(true);
  const [openCards, setOpenCards] = useState(true);
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState("");
  const [templateForm, setTemplateForm] = useState({ name:"", country:"US", postal:"", line1:"", line2:"", city:"", state:"" });
  const [cardForm, setCardForm] = useState({ number:"", month:"", year:"", cvc:"", name:"", email:"", templateId:"" });
  const [removeLast4, setRemoveLast4] = useState("");

  async function load() {
    try {
      const [n, c, t] = await Promise.all([
        api<NodesResponse>("/api/admin/payment/nodes"),
        api<CardsResponse>("/api/admin/payment/cards"),
        api<{templates: BillingTemplate[]}>("/api/admin/payment/billing-templates"),
      ]);
      setNodes(n); setCards(c); setTemplates(t.templates ?? []);
    } catch (e) { setMessage((e as Error).message); }
  }
  useEffect(() => { void load(); }, []);

  async function saveNodes() {
    setBusy("nodes"); setMessage("");
    try {
      const value = JSON.parse(nodeJSON);
      const payload = Array.isArray(value) ? value : value?.outbounds;
      if (!Array.isArray(payload)) throw new Error("请输入节点数组，或包含 outbounds 数组的 JSON。");
      await api("/api/admin/payment/nodes", payload);
      setNodeJSON(""); setMessage("支付节点已保存并加密写入 Vault。"); await load();
    } catch (e) { setMessage((e as Error).message); } finally { setBusy(""); }
  }

  async function probeNodes() {
    setBusy("probe"); setMessage("");
    try {
      const data = await api<{results: Probe[]}>("/api/admin/payment/nodes/probe");
      setProbe(data.results ?? []); setMessage("支付节点探测完成。"); await load();
    } catch (e) { setMessage((e as Error).message); } finally { setBusy(""); }
  }

  async function createTemplate() {
    setBusy("template"); setMessage("");
    try {
      await api("/api/admin/payment/billing-templates", templateForm);
      setTemplateForm({name:"", country:"US", postal:"", line1:"", line2:"", city:"", state:""});
      setMessage("账单地址模板已保存。"); await load();
    } catch (e) { setMessage((e as Error).message); } finally { setBusy(""); }
  }

  function selectedTemplate() {
    return templates.find(t => t.id === cardForm.templateId);
  }

  async function saveCard() {
    setBusy("card"); setMessage("");
    try {
      const t = selectedTemplate();
      if (!t) throw new Error("请选择一个账单地址模板。");
      if (!cardForm.number || !cardForm.month || !cardForm.year || !cardForm.cvc || !cardForm.name || !cardForm.email)
        throw new Error("请完整填写卡号、有效期、CVC、持卡人姓名和邮箱。");
      await api("/api/admin/payment/cards", {
        number: cardForm.number.replace(/\s+/g, ""),
        month: cardForm.month.padStart(2, "0"),
        year: cardForm.year,
        cvc: cardForm.cvc,
        name: cardForm.name,
        email: cardForm.email,
        country: t.country,
        postal: t.postal,
        line1: t.line1,
        line2: t.line2 ?? "",
        city: t.city,
        state: t.state,
      });
      setCardForm({number:"", month:"", year:"", cvc:"", name:"", email:"", templateId:""});
      setMessage("支付卡已保存到加密 Vault。"); await load();
    } catch (e) { setMessage((e as Error).message); } finally { setBusy(""); }
  }

  async function removeCard() {
    setBusy("remove"); setMessage("");
    try {
      if (!/^\d{4}$/.test(removeLast4)) throw new Error("请输入 4 位卡号后四位。");
      await api("/api/admin/payment/cards/remove", {last4: removeLast4});
      setRemoveLast4(""); setMessage("支付卡已删除。"); await load();
    } catch (e) { setMessage((e as Error).message); } finally { setBusy(""); }
  }

  async function unblock() {
    setBusy("unblock"); setMessage("");
    try {
      const d = await api<{unblocked:number}>("/api/admin/payment/cards/unblock");
      setMessage(`已解除 ${d.unblocked} 个支付卡阻断状态。`); await load();
    } catch (e) { setMessage((e as Error).message); } finally { setBusy(""); }
  }

  return <Stack spacing={3} sx={{mb:3}}>
    {message && <Alert severity="info" onClose={() => setMessage("")}>{message}</Alert>}

    <Paper variant="outlined" sx={{p:{xs:2.5,sm:3}}}>
      <Stack direction="row" justifyContent="space-between" alignItems="center">
        <Stack direction="row" spacing={1.5} alignItems="center">
          <NetworkCheckRounded color="primary"/>
          <Box><Typography variant="h2">支付节点管理</Typography><Typography variant="body2" color="text.secondary">Stripe 支付链路使用的独立出口节点。</Typography></Box>
        </Stack>
        <Button onClick={() => setOpenNodes(v=>!v)}>{openNodes ? "收起":"展开"}</Button>
      </Stack>
      <Collapse in={openNodes}><Stack spacing={2.5} sx={{mt:2.5}}>
        <Stack direction={{xs:"column",sm:"row"}} spacing={1}>
          <Chip label={nodes ? `模式：${nodes.mode}`:"读取中"}/><Chip label={`节点 ${nodes?.count??0}`}/>
          <Chip color="success" variant="outlined" label={`可用 ${nodes?.available??0}`}/><Chip color="warning" variant="outlined" label={`冷却 ${nodes?.cooling??0}`}/>
          <Button variant="outlined" startIcon={<NetworkCheckRounded/>} disabled={!!busy} onClick={()=>void probeNodes()}>{busy==="probe"?"探测中…":"立即探测"}</Button>
        </Stack>
        {!!nodes?.nodes.length && <Stack spacing={1}>{nodes.nodes.map(n=><Alert key={n.id} severity="info" icon={false}><b>{n.id}</b> · {n.type} · 出口 IP：{n.egress_ip??"尚未探测"}</Alert>)}</Stack>}
        {!!probe.length && <Stack spacing={1}>{probe.map(p=><Alert key={p.node} severity={p.healthy?"success":"error"} icon={false}><b>{p.node}</b> · {p.type} · Stripe HTTP {p.stripe_http||"—"} · 出口 IP {p.egress_ip??"—"}{p.error?` · ${p.error}`:""}</Alert>)}</Stack>}
        <Divider/>
        <TextField multiline minRows={8} label="支付节点 JSON" value={nodeJSON} onChange={e=>setNodeJSON(e.target.value)}
          placeholder={'支持直接粘贴 { "outbounds": [ ... ] } 或 [ ... ]'} helperText="支持你现有的 outbounds 格式；保存前严格校验，最多 128 个节点。" fullWidth/>
        <Button variant="contained" startIcon={<SaveRounded/>} disabled={!nodeJSON.trim()||!!busy} onClick={()=>void saveNodes()}>{busy==="nodes"?"保存中…":"保存支付节点"}</Button>
      </Stack></Collapse>
    </Paper>

    <Paper variant="outlined" sx={{p:{xs:2.5,sm:3}}}>
      <Stack direction="row" justifyContent="space-between" alignItems="center">
        <Stack direction="row" spacing={1.5} alignItems="center"><CreditCardRounded color="primary"/><Box><Typography variant="h2">支付卡管理</Typography><Typography variant="body2" color="text.secondary">不用填写 JSON；填写卡片信息并选择账单地址模板即可保存。</Typography></Box></Stack>
        <Button onClick={()=>setOpenCards(v=>!v)}>{openCards?"收起":"展开"}</Button>
      </Stack>
      <Collapse in={openCards}><Stack spacing={2.5} sx={{mt:2.5}}>
        {cards?.cards?.length ? cards.cards.map(c=><Alert key={c.last4} severity={c.usable?"info":"error"} icon={false}>卡号 **** {c.last4} · usable: {String(c.usable)}{c.blocked?` · 阻断：${c.blocked}`:""}{c.cooling_seconds?` · 冷却 ${c.cooling_seconds}s`:""}</Alert>) : <Alert severity="warning">当前没有已配置的支付卡。</Alert>}

        <Typography variant="h3">① 账单地址模板</Typography>
        {templates.length>0 && <FormControl fullWidth><InputLabel>选择已有地址模板</InputLabel><Select label="选择已有地址模板" value={cardForm.templateId} onChange={e=>setCardForm(v=>({...v,templateId:e.target.value}))}>{templates.map(t=><MenuItem key={t.id} value={t.id}>{t.name} · {t.city}, {t.state} · {t.country}</MenuItem>)}</Select></FormControl>}
        <Stack direction={{xs:"column",sm:"row"}} spacing={1}>
          <TextField label="模板名称" value={templateForm.name} onChange={e=>setTemplateForm(v=>({...v,name:e.target.value}))} fullWidth/>
          <TextField label="国家代码" value={templateForm.country} onChange={e=>setTemplateForm(v=>({...v,country:e.target.value.toUpperCase().slice(0,2)}))} sx={{width:{sm:180}}}/>
          <TextField label="州/省" value={templateForm.state} onChange={e=>setTemplateForm(v=>({...v,state:e.target.value}))} sx={{width:{sm:180}}/>
        </Stack>
        <Stack direction={{xs:"column",sm:"row"}} spacing={1}>
          <TextField label="城市" value={templateForm.city} onChange={e=>setTemplateForm(v=>({...v,city:e.target.value}))} fullWidth/>
          <TextField label="邮编" value={templateForm.postal} onChange={e=>setTemplateForm(v=>({...v,postal:e.target.value}))} fullWidth/>
        </Stack>
        <TextField label="地址 1" value={templateForm.line1} onChange={e=>setTemplateForm(v=>({...v,line1:e.target.value}))} fullWidth/>
        <TextField label="地址 2（可选）" value={templateForm.line2} onChange={e=>setTemplateForm(v=>({...v,line2:e.target.value}))} fullWidth/>
        <Button variant="outlined" onClick={()=>void createTemplate()} disabled={!!busy || !templateForm.name}>保存地址模板</Button>

        <Divider/>
        <Typography variant="h3">② 输入支付卡</Typography>
        <Stack direction={{xs:"column",sm:"row"}} spacing={1}>
          <TextField label="卡号" value={cardForm.number} onChange={e=>setCardForm(v=>({...v,number:e.target.value}))} fullWidth autoComplete="off"/>
          <TextField label="CVC" value={cardForm.cvc} onChange={e=>setCardForm(v=>({...v,cvc:e.target.value}))} sx={{width:{sm:150}}} autoComplete="off" type="password"/>
        </Stack>
        <Stack direction={{xs:"column",sm:"row"}} spacing={1}>
          <TextField label="有效期 月" value={cardForm.month} onChange={e=>setCardForm(v=>({...v,month:e.target.value.replace(/\D/g,"").slice(0,2)}))} sx={{width:{sm:160}}}/>
          <TextField label="有效期 年" value={cardForm.year} onChange={e=>setCardForm(v=>({...v,year:e.target.value.replace(/\D/g,"").slice(0,4)}))} sx={{width:{sm:160}}}/>
          <TextField label="持卡人姓名" value={cardForm.name} onChange={e=>setCardForm(v=>({...v,name:e.target.value}))} fullWidth/>
          <TextField label="邮箱" value={cardForm.email} onChange={e=>setCardForm(v=>({...v,email:e.target.value}))} fullWidth/>
        </Stack>
        <Button variant="contained" size="large" startIcon={<AddCardRounded/>} disabled={!!busy || !cardForm.templateId} onClick={()=>void saveCard()}>{busy==="card"?"保存中…":"选择模板并保存支付卡"}</Button>

        <Divider/>
        <Stack direction={{xs:"column",sm:"row"}} spacing={1}>
          <TextField label="删除卡号后四位" value={removeLast4} onChange={e=>setRemoveLast4(e.target.value.replace(/\D/g,"").slice(0,4))} sx={{maxWidth:220}}/>
          <Button color="error" variant="outlined" startIcon={<DeleteOutlineRounded/>} disabled={!!busy||removeLast4.length!==4} onClick={()=>void removeCard()}>{busy==="remove"?"删除中…":"删除支付卡"}</Button>
          <Button variant="outlined" startIcon={<LockOpenRounded/>} disabled={!!busy} onClick={()=>void unblock()}>{busy==="unblock"?"处理中…":"解除卡片阻断"}</Button>
        </Stack>
      </Stack></Collapse>
    </Paper>
  </Stack>;
}
