import { useEffect, useState } from "react";
import {
  Alert,
  Box,
  Button,
  Chip,
  Collapse,
  Divider,
  Paper,
  Stack,
  TextField,
  Typography,
} from "@mui/material";
import NetworkCheckRounded from "@mui/icons-material/NetworkCheckRounded";
import CreditCardRounded from "@mui/icons-material/CreditCardRounded";
import AddCardRounded from "@mui/icons-material/AddCardRounded";
import SaveRounded from "@mui/icons-material/SaveRounded";
import DeleteOutlineRounded from "@mui/icons-material/DeleteOutlineRounded";
import LockOpenRounded from "@mui/icons-material/LockOpenRounded";
import { adminApi as api } from "./adminApi";

type NodeStatus = {
  id: string;
  type: string;
  egress_ip?: string;
};
type NodesResponse = {
  mode: string;
  nodes: NodeStatus[];
  count: number;
  available: number;
  cooling: number;
};
type Probe = {
  node: string;
  type: string;
  healthy: boolean;
  stripe_http: number;
  egress_ip?: string;
  error?: string;
};
type Card = Record<string, unknown>;
type CardsResponse = {
  cards: Card[];
  rotation: Record<string, unknown>;
};

const defaultNodeExample = JSON.stringify(
  [
    {
      type: "anytls",
      tag: "payment-1",
      server: "example.com",
      server_port: 443,
      password: "YOUR_PASSWORD",
      tls: { enabled: true, server_name: "example.com" },
    },
  ],
  null,
  2,
);

const defaultCardExample = JSON.stringify(
  [
    {
      number: "CARD_NUMBER",
      month: "10",
      year: "2028",
      cvc: "CVC",
      name: "Card Holder",
      email: "billing@example.com",
      country: "US",
      postal: "90000",
      line1: "Example Street 1",
      line2: "",
      city: "Los Angeles",
      state: "CA",
    },
  ],
  null,
  2,
);

export function PaymentAdminPanel() {
  const [nodes, setNodes] = useState<NodesResponse | null>(null);
  const [cards, setCards] = useState<CardsResponse | null>(null);
  const [nodeJSON, setNodeJSON] = useState("");
  const [cardJSON, setCardJSON] = useState("");
  const [billingJSON, setBillingJSON] = useState("");
  const [removeLast4, setRemoveLast4] = useState("");
  const [probe, setProbe] = useState<Probe[]>([]);
  const [openNodes, setOpenNodes] = useState(true);
  const [openCards, setOpenCards] = useState(true);
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState("");

  async function load() {
    try {
      const [nodeData, cardData] = await Promise.all([
        api<NodesResponse>("/api/admin/payment/nodes"),
        api<CardsResponse>("/api/admin/payment/cards"),
      ]);
      setNodes(nodeData);
      setCards(cardData);
    } catch (error) {
      setMessage((error as Error).message);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  async function saveNodes() {
    setBusy("nodes");
    setMessage("");
    try {
      const value = JSON.parse(nodeJSON);
      if (!Array.isArray(value)) throw new Error("支付节点必须是 JSON 数组。");
      await api("/api/admin/payment/nodes", value);
      setNodeJSON("");
      setMessage("支付节点已保存并加密写入 Vault。");
      await load();
    } catch (error) {
      setMessage((error as Error).message);
    } finally {
      setBusy("");
    }
  }

  async function probeNodes() {
    setBusy("probe");
    setMessage("");
    try {
      const data = await api<{ results: Probe[] }>("/api/admin/payment/nodes/probe");
      setProbe(data.results ?? []);
      setMessage("支付节点探测完成。");
      await load();
    } catch (error) {
      setMessage((error as Error).message);
    } finally {
      setBusy("");
    }
  }

  async function addCards() {
    setBusy("cards");
    setMessage("");
    try {
      const value = JSON.parse(cardJSON);
      await api("/api/admin/payment/cards", value);
      setCardJSON("");
      setMessage("支付卡已保存到加密 Vault。");
      await load();
    } catch (error) {
      setMessage((error as Error).message);
    } finally {
      setBusy("");
    }
  }

  async function replaceCards() {
    setBusy("replace");
    setMessage("");
    try {
      const value = JSON.parse(cardJSON);
      if (!Array.isArray(value)) throw new Error("替换卡池必须是 JSON 数组。");
      await api("/api/admin/payment/cards", value);
      setCardJSON("");
      setMessage("支付卡池已替换。");
      await load();
    } catch (error) {
      setMessage((error as Error).message);
    } finally {
      setBusy("");
    }
  }

  async function removeCard() {
    setBusy("remove");
    setMessage("");
    try {
      if (!/^\d{4}$/.test(removeLast4.trim()))
        throw new Error("请输入 4 位卡号后四位。");
      await api("/api/admin/payment/cards/remove", {
        last4: removeLast4.trim(),
      });
      setRemoveLast4("");
      setMessage("支付卡已删除。");
      await load();
    } catch (error) {
      setMessage((error as Error).message);
    } finally {
      setBusy("");
    }
  }

  async function updateBilling() {
    setBusy("billing");
    setMessage("");
    try {
      const value = JSON.parse(billingJSON);
      await api("/api/admin/payment/cards/billing", value);
      setBillingJSON("");
      setMessage("账单信息已更新。");
      await load();
    } catch (error) {
      setMessage((error as Error).message);
    } finally {
      setBusy("");
    }
  }

  async function unblock() {
    setBusy("unblock");
    setMessage("");
    try {
      const data = await api<{ unblocked: number }>(
        "/api/admin/payment/cards/unblock",
      );
      setMessage(`已解除 ${data.unblocked} 个支付卡阻断状态。`);
      await load();
    } catch (error) {
      setMessage((error as Error).message);
    } finally {
      setBusy("");
    }
  }

  return (
    <Stack spacing={3} sx={{ mb: 3 }}>
      {message && <Alert severity="info" onClose={() => setMessage("")}>{message}</Alert>}

      <Paper variant="outlined" sx={{ p: { xs: 2.5, sm: 3 } }}>
        <Stack direction="row" justifyContent="space-between" alignItems="center" spacing={2}>
          <Stack direction="row" spacing={1.5} alignItems="center">
            <NetworkCheckRounded color="primary" />
            <Box>
              <Typography variant="h2">支付节点管理</Typography>
              <Typography variant="body2" color="text.secondary">
                独立于 X 账号代理，用于 Stripe 支付链路。
              </Typography>
            </Box>
          </Stack>
          <Button onClick={() => setOpenNodes((v) => !v)}>
            {openNodes ? "收起" : "展开"}
          </Button>
        </Stack>

        <Collapse in={openNodes}>
          <Stack spacing={2.5} sx={{ mt: 2.5 }}>
            <Stack direction={{ xs: "column", sm: "row" }} spacing={1}>
              <Chip label={nodes ? `模式：${nodes.mode}` : "读取中"} />
              <Chip label={`节点 ${nodes?.count ?? 0}`} />
              <Chip color="success" variant="outlined" label={`可用 ${nodes?.available ?? 0}`} />
              <Chip color="warning" variant="outlined" label={`冷却 ${nodes?.cooling ?? 0}`} />
              <Button variant="outlined" startIcon={<NetworkCheckRounded />} disabled={busy !== ""} onClick={() => void probeNodes()}>
                {busy === "probe" ? "探测中…" : "立即探测"}
              </Button>
            </Stack>

            {!!nodes?.nodes.length && (
              <Stack spacing={1}>
                {nodes.nodes.map((node) => (
                  <Alert key={node.id} severity="info" icon={false}>
                    <b>{node.id}</b> · {node.type} · 出口 IP：{node.egress_ip ?? "尚未探测"}
                  </Alert>
                ))}
              </Stack>
            )}

            {!!probe.length && (
              <Box>
                <Typography variant="subtitle2" sx={{ mb: 1 }}>最近一次探测</Typography>
                <Stack spacing={1}>
                  {probe.map((item) => (
                    <Alert key={item.node} severity={item.healthy ? "success" : "error"} icon={false}>
                      <b>{item.node}</b> · {item.type} · Stripe HTTP {item.stripe_http || "—"} · 出口 IP {item.egress_ip ?? "—"}
                      {item.error ? ` · ${item.error}` : ""}
                    </Alert>
                  ))}
                </Stack>
              </Box>
            )}

            <Divider />
            <TextField
              multiline
              minRows={8}
              label="支付节点 JSON"
              placeholder={defaultNodeExample}
              value={nodeJSON}
              onChange={(e) => setNodeJSON(e.target.value)}
              helperText="直接填写 payment-outbounds 数组；保存前会严格校验，最多 128 个节点。"
              fullWidth
            />
            <Stack direction={{ xs: "column", sm: "row" }} spacing={1}>
              <Button variant="contained" startIcon={<SaveRounded />} disabled={!nodeJSON.trim() || !!busy} onClick={() => void saveNodes()}>
                {busy === "nodes" ? "保存中…" : "保存支付节点"}
              </Button>
              <Button variant="text" disabled={!!busy} onClick={() => setNodeJSON(defaultNodeExample)}>
                填入示例
              </Button>
            </Stack>
          </Stack>
        </Collapse>
      </Paper>

      <Paper variant="outlined" sx={{ p: { xs: 2.5, sm: 3 } }}>
        <Stack direction="row" justifyContent="space-between" alignItems="center" spacing={2}>
          <Stack direction="row" spacing={1.5} alignItems="center">
            <CreditCardRounded color="primary" />
            <Box>
              <Typography variant="h2">支付卡管理</Typography>
              <Typography variant="body2" color="text.secondary">
                卡片保存在加密 Vault；后台查询只显示脱敏状态。
              </Typography>
            </Box>
          </Stack>
          <Button onClick={() => setOpenCards((v) => !v)}>
            {openCards ? "收起" : "展开"}
          </Button>
        </Stack>

        <Collapse in={openCards}>
          <Stack spacing={2.5} sx={{ mt: 2.5 }}>
            {cards?.cards?.length ? (
              <Stack spacing={1}>
                {cards.cards.map((card, index) => (
                  <Alert key={String(card.id ?? card.last4 ?? index)} severity="info" icon={false}>
                    {Object.entries(card).map(([key, value]) => `${key}: ${String(value)}`).join(" · ")}
                  </Alert>
                ))}
              </Stack>
            ) : (
              <Alert severity="warning">当前没有已配置的支付卡。</Alert>
            )}

            <TextField
              multiline
              minRows={9}
              label="新增支付卡 JSON"
              placeholder={defaultCardExample}
              value={cardJSON}
              onChange={(e) => setCardJSON(e.target.value)}
              helperText="支持单张卡对象或卡片数组。真实卡号/CVC 不要写入 Git、日志或聊天记录。"
              fullWidth
            />
            <Stack direction={{ xs: "column", sm: "row" }} spacing={1}>
              <Button variant="contained" startIcon={<AddCardRounded />} disabled={!cardJSON.trim() || !!busy} onClick={() => void addCards()}>
                {busy === "cards" ? "保存中…" : "新增支付卡"}
              </Button>
              <Button variant="outlined" startIcon={<SaveRounded />} disabled={!cardJSON.trim() || !!busy} onClick={() => void replaceCards()}>
                {busy === "replace" ? "替换中…" : "替换整个卡池"}
              </Button>
              <Button variant="text" disabled={!!busy} onClick={() => setCardJSON(defaultCardExample)}>
                填入示例
              </Button>
            </Stack>

            <Divider />
            <Stack direction={{ xs: "column", sm: "row" }} spacing={1} alignItems={{ sm: "flex-start" }}>
              <TextField
                label="删除卡号后四位"
                value={removeLast4}
                onChange={(e) => setRemoveLast4(e.target.value.replace(/\D/g, "").slice(0, 4))}
                inputProps={{ inputMode: "numeric", maxLength: 4 }}
                sx={{ maxWidth: 220 }}
              />
              <Button color="error" variant="outlined" startIcon={<DeleteOutlineRounded />} disabled={!!busy || !removeLast4} onClick={() => void removeCard()}>
                {busy === "remove" ? "删除中…" : "删除支付卡"}
              </Button>
              <Button variant="outlined" startIcon={<LockOpenRounded />} disabled={!!busy} onClick={() => void unblock()}>
                {busy === "unblock" ? "处理中…" : "解除卡片阻断"}
              </Button>
            </Stack>

            <TextField
              multiline
              minRows={5}
              label="统一账单信息 JSON（可选）"
              placeholder={`{
  "billing_name": "Card Holder",
  "email": "billing@example.com",
  "billing_country": "US",
  "billing_postal_code": "90000",
  "billing_address_line1": "Example Street 1",
  "billing_city": "Los Angeles",
  "billing_state": "CA"
}`}
              value={billingJSON}
              onChange={(e) => setBillingJSON(e.target.value)}
              fullWidth
            />
            <Button variant="outlined" disabled={!billingJSON.trim() || !!busy} onClick={() => void updateBilling()}>
              {busy === "billing" ? "更新中…" : "更新账单信息"}
            </Button>
          </Stack>
        </Collapse>
      </Paper>
    </Stack>
  );
}
