import { useCallback, useEffect, useMemo, useState } from "react";
import {
  ArrowDown,
  ArrowLeft,
  ArrowUp,
  ChevronDown,
  Plus,
  ShieldAlert,
  ShieldCheck,
  Trash2,
} from "lucide-react";
import toast from "react-hot-toast";
import { useNavigate, useSearchParams } from "react-router-dom";

import { AnimatedPage } from "@/components/animated-page";
import { PageLoadingState } from "@/components/page-state";
import { SelectableCard } from "@/components/selectable-card";
import { Card, CardBody } from "@/shadcn-bridge/heroui/card";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Input, Textarea } from "@/shadcn-bridge/heroui/input";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Switch } from "@/shadcn-bridge/heroui/switch";
import { Tab, Tabs } from "@/shadcn-bridge/heroui/tabs";
import {
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
} from "@/shadcn-bridge/heroui/modal";
import {
  createSubscription,
  getProxyNodes,
  getSubscription,
  getSubscriptionTemplates,
  previewSubscription,
  updateSubscription,
  type GeneratorConfig,
  type ProxyGroupSpec,
  type ProxyNode,
  type SubscriptionTemplate,
} from "@/api/enhanced";
import {
  copyText,
  firstKey,
  hostPort,
  protocolLabel,
  saveTextFile,
} from "@/utils/enhanced";

const modalClassNames = {
  base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden",
};

const GROUP_TYPES = [
  { key: "select", label: "手动选择 select" },
  { key: "url-test", label: "自动测速 url-test" },
  { key: "fallback", label: "故障转移 fallback" },
  { key: "load-balance", label: "负载均衡 load-balance" },
];

const BUILTIN_POLICIES = ["DIRECT", "REJECT", "REJECT-DROP", "PASS"];
const AI_GROUP = "🤖 AI 服务";
const SELECT_GROUP = "🚀 节点选择";
const QUICK_RULE_TYPES = [
  "DOMAIN-SUFFIX",
  "DOMAIN",
  "DOMAIN-KEYWORD",
  "IP-CIDR",
  "IP-CIDR6",
  "GEOSITE",
] as const;
type QuickRuleType = (typeof QUICK_RULE_TYPES)[number];

const cloneGroups = (groups: ProxyGroupSpec[]) =>
  groups.map((g) => ({ ...g, proxies: [...(g.proxies || [])] }));

const initialNodeIds = (requested: string | null, nodes: ProxyNode[]) => {
  if (requested === null)
    return nodes.filter((node) => node.enabled).map((node) => node.id);

  const existing = new Set(nodes.map((node) => node.id));
  const matched = new Set<number>();

  for (const segment of requested.split(",")) {
    const value = segment.trim();
    const id = Number(value);

    if (
      /^\d+$/.test(value) &&
      Number.isSafeInteger(id) &&
      id > 0 &&
      existing.has(id)
    ) {
      matched.add(id);
    }
  }

  return [...matched];
};

const groupMayUseDirect = (group: ProxyGroupSpec) =>
  group.name.trim() === AI_GROUP &&
  group.proxies.some((member) =>
    ["DIRECT", SELECT_GROUP].includes(member.trim()),
  );

const isIPv6Literal = (server: string) => {
  const [address] = server
    .trim()
    .replace(/^\[(.*)\]$/, "$1")
    .split("%");

  if (!address.includes(":") || !/^[\da-f:.]+$/i.test(address)) return false;
  try {
    return !new URL(`http://[${address}]/`).hostname.startsWith("[::ffff:");
  } catch {
    return false;
  }
};

const targetOfRule = (rule: string) => {
  const parts = rule
    .replace(/^-\s*/, "")
    .split(",")
    .map((part) => part.trim());
  const type = parts[0].toUpperCase();

  if (parts.length < 2) return "";
  if (type === "MATCH" || type === "FINAL") return parts[1];
  let target = parts.length - 1;

  while (
    target > 1 &&
    ["no-resolve", "src"].includes(parts[target].toLowerCase())
  ) {
    target--;
  }

  return parts[target];
};

const udpGuardLabel = (rule: string, target: string) => {
  const normalized = rule
    .replace(/^-\s*/, "")
    .replace(/\s+/g, "")
    .toUpperCase();

  if (target !== "REJECT" || !normalized.startsWith("AND,((NETWORK,UDP),"))
    return "";
  if (
    normalized.includes("CLAUDE.AI") ||
    normalized.includes("ANTHROPIC.COM")
  ) {
    return "Claude UDP 防泄露兜底";
  }

  return normalized.includes("GEOSITE,CATEGORY-AI-!CN")
    ? "AI UDP 防泄露兜底"
    : "";
};

export default function SubGeneratorPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const id = Number(searchParams.get("id"));
  const editId = Number.isSafeInteger(id) && id > 0 ? id : 0;
  const requestedNodes = editId === 0 ? searchParams.get("nodes") : null;

  const [loading, setLoading] = useState(true);
  const [nodes, setNodes] = useState<ProxyNode[]>([]);
  const [allTags, setAllTags] = useState<string[]>([]);
  const [templates, setTemplates] = useState<SubscriptionTemplate[]>([]);
  const [defaultBaseConfig, setDefaultBaseConfig] = useState("");

  const [name, setName] = useState("");
  const [fileName, setFileName] = useState("");
  const [remark, setRemark] = useState("");
  const [enabled, setEnabled] = useState(true);

  const [allNodes, setAllNodes] = useState(false);
  const [tags, setTags] = useState<string[]>([]);
  const [selectedNodeIds, setSelectedNodeIds] = useState<number[]>([]);
  const [nodeKeyword, setNodeKeyword] = useState("");
  const [nodeTagFilter, setNodeTagFilter] = useState("");

  const [templateId, setTemplateId] = useState("standard");
  const [groups, setGroups] = useState<ProxyGroupSpec[]>([]);
  const [rulesText, setRulesText] = useState("");
  const [baseConfig, setBaseConfig] = useState("");
  const [ruleProviders, setRuleProviders] = useState("");
  const [ipv4Only, setIPv4Only] = useState(false);
  const [activeTab, setActiveTab] = useState("nodes");
  const [expandedGroup, setExpandedGroup] = useState<number | null>(null);
  const [ruleView, setRuleView] = useState<"list" | "source">("list");
  const [quickRuleType, setQuickRuleType] =
    useState<QuickRuleType>("DOMAIN-SUFFIX");
  const [quickRuleValue, setQuickRuleValue] = useState("");
  const [quickRuleTarget, setQuickRuleTarget] = useState("");

  const [previewOpen, setPreviewOpen] = useState(false);
  const [previewContent, setPreviewContent] = useState("");
  const [previewWarnings, setPreviewWarnings] = useState<string[]>([]);
  const [previewing, setPreviewing] = useState(false);
  const [saving, setSaving] = useState(false);

  const applyTemplate = useCallback(
    (id: string, list: SubscriptionTemplate[] = templates) => {
      const tpl = list.find((t) => t.id === id);

      if (!tpl) return;
      setTemplateId(id);
      setGroups(cloneGroups(tpl.groups));
      setRulesText(tpl.rules.join("\n"));
      setBaseConfig("");
      setExpandedGroup(null);
    },
    [templates],
  );

  useEffect(() => {
    let cancelled = false;

    (async () => {
      setLoading(true);
      try {
        const [nodeRes, tplRes] = await Promise.all([
          getProxyNodes(),
          getSubscriptionTemplates(),
        ]);

        if (cancelled) return;
        const nodeList = nodeRes.code === 0 ? (nodeRes.data?.nodes ?? []) : [];

        setNodes(nodeList);
        setAllTags(nodeRes.data?.tags || []);
        const tplList = tplRes.code === 0 ? (tplRes.data?.templates ?? []) : [];

        setTemplates(tplList);
        setDefaultBaseConfig(tplRes.data?.defaultBaseConfig || "");

        const subRes = editId > 0 ? await getSubscription(editId) : null;

        if (cancelled) return;
        if (subRes && (subRes.code !== 0 || !subRes.data)) {
          toast.error(subRes.msg || "订阅不存在");
          navigate("/subscription", { replace: true });

          return;
        }
        const subscription = subRes?.data;
        const config = subscription?.genConfig;
        const template =
          tplList.find(
            (item) => item.id === (config?.template || "standard"),
          ) ?? (subscription ? undefined : tplList[0]);

        setName(subscription?.name ?? "");
        setFileName(
          subscription?.fileName?.replace(/\.(yaml|yml|txt)$/i, "") ?? "",
        );
        setRemark(subscription?.remark ?? "");
        setEnabled(subscription?.enabled ?? true);
        setAllNodes(Boolean(config?.allNodes));
        setTags(config?.tags ?? []);
        setSelectedNodeIds(
          subscription
            ? (config?.nodeIds ?? [])
            : initialNodeIds(requestedNodes, nodeList),
        );
        setTemplateId(config?.template || template?.id || "standard");
        setGroups(
          cloneGroups(
            config?.groups?.length ? config.groups : (template?.groups ?? []),
          ),
        );
        setRulesText(
          (config?.rules?.length ? config.rules : (template?.rules ?? [])).join(
            "\n",
          ),
        );
        setBaseConfig(config?.baseConfig ?? "");
        setRuleProviders(config?.ruleProviders ?? "");
        setIPv4Only(Boolean(config?.ipv4Only));
        setActiveTab("nodes");
        setExpandedGroup(null);
        setRuleView("list");
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [editId, navigate, requestedNodes]);

  const enabledNodes = useMemo(() => nodes.filter((n) => n.enabled), [nodes]);

  const visibleNodes = useMemo(() => {
    const kw = nodeKeyword.trim().toLowerCase();

    return nodes.filter((n) => {
      if (nodeTagFilter && !n.tags.includes(nodeTagFilter)) return false;
      if (!kw) return true;

      return (
        n.name.toLowerCase().includes(kw) || n.server.toLowerCase().includes(kw)
      );
    });
  }, [nodes, nodeKeyword, nodeTagFilter]);

  const selectedSet = useMemo(
    () => new Set(selectedNodeIds),
    [selectedNodeIds],
  );

  const effectiveNodes = useMemo(() => {
    if (allNodes) {
      return enabledNodes.filter(
        (n) => tags.length === 0 || n.tags.some((t) => tags.includes(t)),
      );
    }

    const byId = new Map(enabledNodes.map((node) => [node.id, node]));

    return selectedNodeIds
      .map((id) => byId.get(id))
      .filter((node): node is ProxyNode => node !== undefined);
  }, [allNodes, enabledNodes, tags, selectedNodeIds]);

  const aiRisk = groups.some(groupMayUseDirect);
  const ipv6Nodes = ipv4Only
    ? effectiveNodes.filter((node) => isIPv6Literal(node.server))
    : [];
  const hasWarnings = aiRisk || ipv6Nodes.length > 0;
  const ruleTargets = [
    ...new Set([
      ...groups
        .filter((group) => !group.hidden)
        .map((group) => group.name.trim())
        .filter(Boolean),
      "DIRECT",
      "REJECT",
    ]),
  ];
  const activeRuleTarget = ruleTargets.includes(quickRuleTarget)
    ? quickRuleTarget
    : ruleTargets[0];
  const ruleRows = useMemo(
    () =>
      rulesText.split("\n").flatMap((line, lineIndex) => {
        const rule = line.trim();

        return rule && !rule.startsWith("#") ? [{ rule, lineIndex }] : [];
      }),
    [rulesText],
  );

  const toggleNode = (id: number) => {
    setSelectedNodeIds((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id],
    );
  };

  const selectVisible = (select: boolean) => {
    const ids = new Set(visibleNodes.map((node) => node.id));

    setSelectedNodeIds((prev) =>
      select
        ? [...new Set([...prev, ...ids])]
        : prev.filter((id) => !ids.has(id)),
    );
  };

  // ─── 代理组编辑 ───────────────────────────────────────────

  const updateGroup = (index: number, patch: Partial<ProxyGroupSpec>) => {
    setGroups((prev) =>
      prev.map((g, i) => (i === index ? { ...g, ...patch } : g)),
    );
  };

  const renameGroup = (index: number, newName: string) => {
    setGroups((prev) => {
      const oldName = prev[index]?.name;

      return prev.map((g, i) => {
        if (i === index) return { ...g, name: newName };

        return {
          ...g,
          proxies: g.proxies.map((m) => (m === oldName ? newName : m)),
        };
      });
    });
    setRulesText((prev) =>
      prev
        .split("\n")
        .map((line) => {
          const oldName = groups[index]?.name;

          if (!oldName) return line;
          const parts = line.split(",");

          return parts
            .map((p) => (p.trim() === oldName ? newName : p))
            .join(",");
        })
        .join("\n"),
    );
  };

  const moveGroup = (index: number, delta: number) => {
    const target = index + delta;

    if (target < 0 || target >= groups.length) return;
    setGroups((prev) => {
      const next = [...prev];
      const [item] = next.splice(index, 1);

      next.splice(target, 0, item);

      return next;
    });
    setExpandedGroup(target);
  };

  const removeGroup = (index: number) => {
    setGroups((prev) => {
      const removed = prev[index]?.name;

      return prev
        .filter((_, i) => i !== index)
        .map((g) => ({
          ...g,
          proxies: g.proxies.filter((m) => m !== removed),
        }));
    });
    setExpandedGroup(null);
  };

  const addGroup = () => {
    let base = "新代理组";
    let n = 1;
    const names = new Set(groups.map((g) => g.name));

    while (names.has(n === 1 ? base : `${base} ${n}`)) n++;
    base = n === 1 ? base : `${base} ${n}`;
    setGroups((prev) => [
      ...prev,
      { name: base, type: "select", proxies: [], includeAllNodes: true },
    ]);
    setExpandedGroup(groups.length);
  };

  const memberOptions = (selfName: string) => {
    const groupNames = groups
      .map((g) => g.name)
      .filter((n) => n && n !== selfName);
    const nodeNames = effectiveNodes.map((n) => n.name);

    return [
      ...groupNames.map((n) => ({ key: n, label: `代理组 · ${n}` })),
      ...BUILTIN_POLICIES.map((p) => ({ key: p, label: p })),
      ...nodeNames.map((n) => ({ key: n, label: `节点 · ${n}` })),
    ];
  };

  const addRule = () => {
    const value = quickRuleValue.trim();

    if (!value || value.includes(",") || activeRuleTarget.includes(",")) {
      toast.error(!value ? "请填写规则值" : "规则值和目标策略不能包含逗号");

      return;
    }

    const option =
      quickRuleType === "IP-CIDR" || quickRuleType === "IP-CIDR6"
        ? ",no-resolve"
        : "";
    const rule = `${quickRuleType},${value},${activeRuleTarget}${option}`;

    setRulesText((text) => (text ? `${rule}\n${text}` : rule));
    setQuickRuleValue("");
  };

  const moveRule = (index: number, delta: number) => {
    const target = ruleRows[index + delta];

    if (!target) return;
    const { lineIndex } = ruleRows[index];

    setRulesText((text) => {
      const lines = text.split("\n");

      [lines[lineIndex], lines[target.lineIndex]] = [
        lines[target.lineIndex],
        lines[lineIndex],
      ];

      return lines.join("\n");
    });
  };

  const removeRule = (lineIndex: number) =>
    setRulesText((text) =>
      text
        .split("\n")
        .filter((_, index) => index !== lineIndex)
        .join("\n"),
    );

  const buildConfig = (): GeneratorConfig => ({
    nodeIds: allNodes ? [] : selectedNodeIds,
    allNodes,
    tags: allNodes ? tags : [],
    template: templateId,
    groups: groups.map((g) => ({
      ...g,
      name: g.name.trim(),
      interval: g.interval ? Number(g.interval) : undefined,
      tolerance: g.tolerance ? Number(g.tolerance) : undefined,
    })),
    rules: rulesText
      .split("\n")
      .map((line) => line.trim())
      .filter(Boolean),
    baseConfig: baseConfig.trim(),
    ruleProviders: ruleProviders.trim(),
    ipv4Only,
  });

  const validate = () => {
    if (effectiveNodes.length === 0) {
      toast.error("请至少选择一个启用的节点");

      return false;
    }
    if (groups.length === 0) {
      toast.error("至少需要一个代理组");

      return false;
    }
    const names = groups.map((g) => g.name.trim());

    if (names.some((n) => !n)) {
      toast.error("代理组名称不能为空");

      return false;
    }
    if (new Set(names).size !== names.length) {
      toast.error("代理组名称不能重复");

      return false;
    }

    return true;
  };

  const preview = async () => {
    if (!validate()) return;
    setPreviewing(true);
    try {
      const res = await previewSubscription(buildConfig());

      if (res.code === 0 && res.data) {
        setPreviewContent(res.data.content);
        setPreviewWarnings(res.data.warnings || []);
        setPreviewOpen(true);
      } else {
        toast.error(res.msg || "生成失败");
      }
    } finally {
      setPreviewing(false);
    }
  };

  const save = async () => {
    if (!name.trim()) {
      toast.error("请填写订阅名称");

      return;
    }
    if (!validate()) return;
    setSaving(true);
    try {
      const payload = {
        id: editId > 0 ? editId : undefined,
        name: name.trim(),
        remark,
        type: "generate",
        fileName: fileName.trim(),
        genConfig: buildConfig(),
        enabled,
        autoUpdate: 0,
      };
      const res =
        editId > 0
          ? await updateSubscription(payload)
          : await createSubscription(payload);

      if (res.code === 0) {
        toast.success("订阅已保存");
        navigate("/subscription");
      } else {
        toast.error(res.msg || "保存失败");
      }
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return <PageLoadingState message="正在加载..." />;
  }

  return (
    <AnimatedPage className="px-3 pb-8 lg:px-6">
      <header className="sticky top-0 z-20 -mx-3 border-b border-divider bg-background/95 px-3 py-2.5 backdrop-blur lg:-mx-6 lg:px-6">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <Button
              className="px-1.5"
              size="sm"
              startContent={<ArrowLeft aria-hidden="true" size={15} />}
              variant="light"
              onPress={() => navigate("/subscription")}
            >
              返回
            </Button>
            <h1 className="text-base font-semibold text-foreground">
              {editId > 0 ? "编辑生成订阅" : "新建生成订阅"}
            </h1>
            <Chip
              className={
                hasWarnings
                  ? "gap-1 bg-warning/15 text-warning-700 dark:text-warning-300"
                  : "gap-1 bg-success/15 text-success-700 dark:text-success-300"
              }
              size="sm"
              title="检查 AI 代理组和 IPv6 节点。完整配置告警见预览。"
              variant="flat"
            >
              {hasWarnings ? (
                <ShieldAlert size={12} />
              ) : (
                <ShieldCheck size={12} />
              )}
              {hasWarnings ? "配置需检查" : "DNS 分流 · 防直连"}
            </Chip>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <label className="flex items-center gap-1.5 rounded-lg border border-divider px-2 py-1 text-xs">
              仅 IPv4
              <Switch isSelected={ipv4Only} onValueChange={setIPv4Only} />
            </label>
            <Button
              isLoading={previewing}
              size="sm"
              variant="flat"
              onPress={preview}
            >
              预览
            </Button>
            <Button color="primary" isLoading={saving} size="sm" onPress={save}>
              保存订阅
            </Button>
          </div>
        </div>
        {hasWarnings && (
          <div
            className="mt-2 space-y-1 rounded-lg border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning-800 dark:text-warning-200"
            role="status"
          >
            {aiRisk && (
              <p>
                「{AI_GROUP}」包含 DIRECT 或引用「{SELECT_GROUP}」，可能走直连。
              </p>
            )}
            {ipv6Nodes.length > 0 && (
              <p>
                已开启「仅 IPv4」，所选的 {ipv6Nodes.length} 个节点使用 IPv6
                地址（
                {ipv6Nodes
                  .slice(0, 2)
                  .map((node) => node.name)
                  .join("、")}
                {ipv6Nodes.length > 2 ? "等" : ""}），可能无法连接。
              </p>
            )}
          </div>
        )}
      </header>

      <div className="space-y-4 pt-4">
        <Card className="border-divider">
          <CardBody className="grid grid-cols-2 gap-3 p-3 md:grid-cols-[repeat(3,minmax(0,1fr))_auto] md:items-end md:p-4">
            <Input
              isRequired
              label="订阅名称"
              size="sm"
              value={name}
              variant="bordered"
              onChange={(event) => setName(event.target.value)}
            />
            <Input
              label="文件名（可选）"
              placeholder="默认使用订阅名称"
              size="sm"
              value={fileName}
              variant="bordered"
              onChange={(event) => setFileName(event.target.value)}
            />
            <Input
              label="备注"
              size="sm"
              value={remark}
              variant="bordered"
              onChange={(event) => setRemark(event.target.value)}
            />
            <label
              className="flex items-center gap-2 self-end py-1.5 text-sm"
              title="启用后可下载"
            >
              启用
              <Switch isSelected={enabled} onValueChange={setEnabled} />
            </label>
          </CardBody>
        </Card>

        <Card className="border-divider">
          <CardBody className="p-2 pt-2 md:p-3 md:pt-3 [&_[data-slot=tabs-trigger]]:min-w-0 [&_[data-slot=tabs-trigger]]:px-1 sm:[&_[data-slot=tabs-trigger]]:px-2">
            <Tabs
              aria-label="订阅配置"
              selectedKey={activeTab}
              onSelectionChange={(key) => setActiveTab(String(key))}
            >
              <Tab
                key="nodes"
                title={
                  <span className="text-[10px] min-[360px]:text-xs sm:text-sm">
                    选择节点 ({effectiveNodes.length})
                  </span>
                }
              >
                <div className="space-y-3 px-1 py-3 sm:px-2">
                  <div
                    aria-label="节点选择模式"
                    className="flex flex-wrap items-center gap-2"
                    role="group"
                  >
                    <Button
                      aria-pressed={!allNodes}
                      color={!allNodes ? "primary" : "default"}
                      size="sm"
                      variant="flat"
                      onPress={() => setAllNodes(false)}
                    >
                      手动选择
                    </Button>
                    <Button
                      aria-pressed={allNodes}
                      color={allNodes ? "primary" : "default"}
                      size="sm"
                      variant="flat"
                      onPress={() => setAllNodes(true)}
                    >
                      全部启用节点
                    </Button>
                  </div>
                  {allNodes ? (
                    <div className="max-w-md">
                      <Select
                        description="留空时包含所有启用节点，新启用的节点会自动加入。"
                        label="按标签筛选（可选）"
                        placeholder="全部标签"
                        selectedKeys={tags}
                        selectionMode="multiple"
                        size="sm"
                        onSelectionChange={(keys) =>
                          setTags(Array.from(keys).map(String))
                        }
                      >
                        {allTags.map((tag) => (
                          <SelectItem key={tag}>{tag}</SelectItem>
                        ))}
                      </Select>
                    </div>
                  ) : nodes.length === 0 ? (
                    <p className="py-4 text-sm text-default-500">
                      请先在
                      <button
                        className="mx-1 text-primary"
                        type="button"
                        onClick={() => navigate("/proxy-node")}
                      >
                        代理节点
                      </button>
                      添加或导入节点。
                    </p>
                  ) : (
                    <>
                      <div className="flex flex-wrap items-center gap-2">
                        <div className="min-w-32 flex-1 sm:max-w-60">
                          <Input
                            aria-label="搜索节点"
                            placeholder="搜索名称或地址"
                            size="sm"
                            type="search"
                            value={nodeKeyword}
                            variant="bordered"
                            onChange={(event) =>
                              setNodeKeyword(event.target.value)
                            }
                          />
                        </div>
                        <div className="w-28 [&_label]:sr-only">
                          <Select
                            label="标签筛选"
                            placeholder="全部标签"
                            selectedKeys={nodeTagFilter ? [nodeTagFilter] : []}
                            size="sm"
                            onSelectionChange={(keys) =>
                              setNodeTagFilter(firstKey(keys))
                            }
                          >
                            {allTags.map((tag) => (
                              <SelectItem key={tag}>{tag}</SelectItem>
                            ))}
                          </Select>
                        </div>
                        <Button
                          size="sm"
                          variant="flat"
                          onPress={() => selectVisible(true)}
                        >
                          全选当前
                        </Button>
                        <Button
                          size="sm"
                          variant="flat"
                          onPress={() => selectVisible(false)}
                        >
                          取消当前
                        </Button>
                      </div>
                      {visibleNodes.length === 0 ? (
                        <p className="py-4 text-sm text-default-500">
                          没有符合筛选条件的节点。
                        </p>
                      ) : (
                        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
                          {visibleNodes.map((node) => (
                            <SelectableCard
                              key={node.id}
                              className={`min-w-0 items-center ${node.enabled ? "" : "opacity-60"}`}
                              selected={selectedSet.has(node.id)}
                              onToggle={() => toggleNode(node.id)}
                            >
                              <Checkbox
                                aria-hidden="true"
                                className="pointer-events-none"
                                isSelected={selectedSet.has(node.id)}
                                tabIndex={-1}
                              />
                              <div className="min-w-0 flex-1">
                                <div
                                  className="truncate text-sm"
                                  title={node.name}
                                >
                                  {node.name}
                                </div>
                                <div className="truncate font-mono text-xs text-default-500">
                                  {protocolLabel(node.protocol)} ·{" "}
                                  {hostPort(node.server, node.port)}
                                  {!node.enabled && "（已停用）"}
                                </div>
                                {node.source === "relay" && (
                                  <div
                                    className="truncate text-xs text-default-500"
                                    title={`源节点：${node.parentName || "-"} · 隧道：${node.tunnelName || "-"}`}
                                  >
                                    中转 · {node.parentName || "-"} ·{" "}
                                    {node.tunnelName || "-"}
                                  </div>
                                )}
                              </div>
                            </SelectableCard>
                          ))}
                        </div>
                      )}
                    </>
                  )}
                </div>
              </Tab>

              <Tab
                key="groups"
                title={
                  <span className="text-[10px] min-[360px]:text-xs sm:text-sm">
                    代理组 ({groups.length})
                  </span>
                }
              >
                <div className="space-y-3 px-1 py-3 sm:px-2">
                  <div className="flex flex-wrap items-end gap-2">
                    <div className="w-36">
                      <Select
                        label="模板"
                        selectedKeys={[templateId]}
                        size="sm"
                        onSelectionChange={(keys) =>
                          setTemplateId(firstKey(keys) || "standard")
                        }
                      >
                        {templates.map((template) => (
                          <SelectItem key={template.id}>
                            {template.name}
                          </SelectItem>
                        ))}
                      </Select>
                    </div>
                    <Button
                      isDisabled={
                        !templates.some(
                          (template) => template.id === templateId,
                        )
                      }
                      size="sm"
                      title="套用模板会替换当前代理组和规则"
                      variant="flat"
                      onPress={() => applyTemplate(templateId)}
                    >
                      套用模板
                    </Button>
                    <Button
                      color="primary"
                      size="sm"
                      startContent={<Plus aria-hidden="true" size={14} />}
                      variant="flat"
                      onPress={addGroup}
                    >
                      添加代理组
                    </Button>
                  </div>
                  {groups.length === 0 && (
                    <p className="py-3 text-sm text-default-500">
                      套用模板或添加一个代理组。
                    </p>
                  )}
                  <div className="space-y-1.5">
                    {groups.map((group, index) => {
                      const expanded = expandedGroup === index;
                      const aiGroup = group.name.trim() === AI_GROUP;
                      const directRisk = groupMayUseDirect(group);
                      const options = expanded ? memberOptions(group.name) : [];
                      const autoGroup = [
                        "url-test",
                        "fallback",
                        "load-balance",
                      ].includes(group.type);
                      const summary = [
                        ...group.proxies,
                        ...(group.includeAllNodes
                          ? [
                              group.filter
                                ? `+ 节点（${group.filter}）`
                                : "+ 节点",
                            ]
                          : []),
                      ].join(" · ");
                      const editorId = `proxy-group-editor-${index}`;

                      return (
                        <div
                          key={index}
                          className={`rounded-lg border ${directRisk ? "border-warning/70" : "border-divider"}`}
                        >
                          <button
                            aria-controls={expanded ? editorId : undefined}
                            aria-expanded={expanded}
                            className="flex w-full cursor-pointer items-center gap-1.5 rounded-lg px-2 py-2 text-left hover:bg-default-100/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring sm:gap-2 sm:px-3"
                            type="button"
                            onClick={() =>
                              setExpandedGroup(expanded ? null : index)
                            }
                          >
                            <span
                              aria-hidden="true"
                              className="w-5 shrink-0 text-center font-mono text-xs text-default-500"
                            >
                              {index + 1}
                            </span>
                            <span
                              className="min-w-0 truncate text-sm font-medium"
                              title={group.name}
                            >
                              {group.name || "未命名"}
                            </span>
                            <Chip
                              className="shrink-0 bg-default-100 px-1.5 font-mono text-default-600"
                              size="sm"
                              variant="flat"
                            >
                              {group.type}
                            </Chip>
                            {group.hidden && (
                              <Chip
                                className="shrink-0 bg-default-100 px-1.5 text-default-600"
                                size="sm"
                                variant="flat"
                              >
                                隐藏
                              </Chip>
                            )}
                            {aiGroup && (
                              <Chip
                                className={
                                  directRisk
                                    ? "shrink-0 bg-warning/15 px-1.5 text-warning-700 dark:text-warning-300"
                                    : "shrink-0 bg-success/15 px-1.5 text-success-700 dark:text-success-300"
                                }
                                size="sm"
                                variant="flat"
                              >
                                {directRisk ? "可走直连" : "防直连"}
                              </Chip>
                            )}
                            <span
                              className="ml-auto hidden min-w-0 max-w-[45%] truncate text-xs text-default-500 sm:block"
                              title={summary}
                            >
                              {summary || "无成员"}
                            </span>
                            <ChevronDown
                              aria-hidden="true"
                              className={`ml-auto shrink-0 text-default-500 transition-transform sm:ml-0 ${expanded ? "rotate-180" : ""}`}
                              size={15}
                            />
                          </button>
                          {expanded && (
                            <div
                              className="space-y-3 border-t border-divider px-3 py-3"
                              id={editorId}
                            >
                              <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
                                <Input
                                  label="名称"
                                  size="sm"
                                  value={group.name}
                                  variant="bordered"
                                  onChange={(event) =>
                                    renameGroup(index, event.target.value)
                                  }
                                />
                                <Select
                                  label="类型"
                                  selectedKeys={[group.type]}
                                  size="sm"
                                  onSelectionChange={(keys) =>
                                    updateGroup(index, {
                                      type: firstKey(keys) || "select",
                                    })
                                  }
                                >
                                  {GROUP_TYPES.map((type) => (
                                    <SelectItem key={type.key}>
                                      {type.label}
                                    </SelectItem>
                                  ))}
                                </Select>
                                <Select
                                  label="成员（代理组 / 策略 / 节点）"
                                  placeholder="未选择"
                                  selectedKeys={group.proxies.filter((member) =>
                                    options.some(
                                      (option) => option.key === member,
                                    ),
                                  )}
                                  selectionMode="multiple"
                                  size="sm"
                                  onSelectionChange={(keys) => {
                                    const chosen = Array.from(keys).map(String);
                                    const kept = group.proxies.filter(
                                      (member) => chosen.includes(member),
                                    );

                                    updateGroup(index, {
                                      proxies: [
                                        ...kept,
                                        ...chosen.filter(
                                          (member) => !kept.includes(member),
                                        ),
                                      ],
                                    });
                                  }}
                                >
                                  {options.map((option) => (
                                    <SelectItem key={option.key}>
                                      {option.label}
                                    </SelectItem>
                                  ))}
                                </Select>
                              </div>
                              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
                                {group.includeAllNodes && (
                                  <Input
                                    label="节点筛选正则"
                                    placeholder="可选，例如 香港|HK"
                                    size="sm"
                                    value={group.filter || ""}
                                    variant="bordered"
                                    onChange={(event) =>
                                      updateGroup(index, {
                                        filter: event.target.value,
                                      })
                                    }
                                  />
                                )}
                                {autoGroup && (
                                  <>
                                    <Input
                                      label="测速地址"
                                      placeholder="默认 gstatic generate_204"
                                      size="sm"
                                      value={group.url || ""}
                                      variant="bordered"
                                      onChange={(event) =>
                                        updateGroup(index, {
                                          url: event.target.value,
                                        })
                                      }
                                    />
                                    <Input
                                      label="间隔（秒）"
                                      placeholder="300"
                                      size="sm"
                                      type="number"
                                      value={
                                        group.interval
                                          ? String(group.interval)
                                          : ""
                                      }
                                      variant="bordered"
                                      onChange={(event) =>
                                        updateGroup(index, {
                                          interval:
                                            Number(event.target.value) ||
                                            undefined,
                                        })
                                      }
                                    />
                                  </>
                                )}
                              </div>
                              <div className="flex flex-wrap items-center justify-between gap-2">
                                <Checkbox
                                  isSelected={group.includeAllNodes}
                                  onValueChange={(value) =>
                                    updateGroup(index, {
                                      includeAllNodes: value,
                                    })
                                  }
                                >
                                  包含节点
                                </Checkbox>
                                <div className="flex gap-1">
                                  <Button
                                    isDisabled={index === 0}
                                    size="sm"
                                    variant="light"
                                    onPress={() => moveGroup(index, -1)}
                                  >
                                    上移
                                  </Button>
                                  <Button
                                    isDisabled={index === groups.length - 1}
                                    size="sm"
                                    variant="light"
                                    onPress={() => moveGroup(index, 1)}
                                  >
                                    下移
                                  </Button>
                                  <Button
                                    color="danger"
                                    size="sm"
                                    variant="light"
                                    onPress={() => removeGroup(index)}
                                  >
                                    删除
                                  </Button>
                                </div>
                              </div>
                            </div>
                          )}
                        </div>
                      );
                    })}
                  </div>
                </div>
              </Tab>

              <Tab
                key="rules"
                title={
                  <span className="text-[10px] min-[360px]:text-xs sm:text-sm">
                    规则 ({ruleRows.length})
                  </span>
                }
              >
                <div className="space-y-3 px-1 py-3 sm:px-2">
                  <form
                    className="grid items-end gap-2 sm:grid-cols-2 xl:grid-cols-[9rem_minmax(0,1.5fr)_minmax(0,1fr)_auto_auto]"
                    onSubmit={(event) => {
                      event.preventDefault();
                      addRule();
                    }}
                  >
                    <Select
                      label="规则类型"
                      selectedKeys={[quickRuleType]}
                      size="sm"
                      onSelectionChange={(keys) =>
                        setQuickRuleType(
                          (firstKey(keys) ||
                            QUICK_RULE_TYPES[0]) as QuickRuleType,
                        )
                      }
                    >
                      {QUICK_RULE_TYPES.map((type) => (
                        <SelectItem key={type}>{type}</SelectItem>
                      ))}
                    </Select>
                    <Input
                      label="规则值"
                      placeholder="域名、网段或规则集"
                      size="sm"
                      value={quickRuleValue}
                      variant="bordered"
                      onChange={(event) =>
                        setQuickRuleValue(event.target.value)
                      }
                    />
                    <Select
                      label="目标策略"
                      selectedKeys={[activeRuleTarget]}
                      size="sm"
                      onSelectionChange={(keys) =>
                        setQuickRuleTarget(firstKey(keys))
                      }
                    >
                      {ruleTargets.map((target) => (
                        <SelectItem key={target}>{target}</SelectItem>
                      ))}
                    </Select>
                    <div className="flex flex-wrap items-center gap-2 sm:contents">
                      <Button
                        className="justify-self-start"
                        color="primary"
                        size="sm"
                        type="submit"
                        variant="flat"
                      >
                        添加规则
                      </Button>
                      <div
                        aria-label="规则视图"
                        className="flex w-fit gap-0.5 justify-self-end rounded-lg border border-divider p-0.5 sm:col-span-2 xl:col-span-1"
                        role="group"
                      >
                        <Button
                          aria-pressed={ruleView === "list"}
                          className="h-7 px-2"
                          size="sm"
                          variant={ruleView === "list" ? "flat" : "light"}
                          onPress={() => setRuleView("list")}
                        >
                          列表
                        </Button>
                        <Button
                          aria-pressed={ruleView === "source"}
                          className="h-7 px-2"
                          size="sm"
                          variant={ruleView === "source" ? "flat" : "light"}
                          onPress={() => setRuleView("source")}
                        >
                          源码
                        </Button>
                      </div>
                    </div>
                  </form>
                  {ruleView === "source" ? (
                    <Textarea
                      className="font-mono text-xs leading-relaxed"
                      description="每行一条 Mihomo 规则。生成时会校验目标策略并补齐 MATCH。"
                      label="规则源码"
                      minRows={12}
                      value={rulesText}
                      variant="bordered"
                      onChange={(event) => setRulesText(event.target.value)}
                    />
                  ) : ruleRows.length === 0 ? (
                    <p className="py-3 text-sm text-default-500">
                      可快捷添加规则，或在源码中批量粘贴。
                    </p>
                  ) : (
                    <ol className="space-y-1.5">
                      {ruleRows.map(({ rule, lineIndex }, index) => {
                        const target = targetOfRule(rule);
                        const guard = udpGuardLabel(rule, target);

                        return (
                          <li
                            key={`${lineIndex}:${rule}`}
                            className="grid grid-cols-[1.25rem_minmax(0,1fr)] items-start gap-x-1.5 gap-y-1 rounded-lg border border-divider px-2 py-1.5 text-xs sm:grid-cols-[1.5rem_minmax(0,1fr)_auto] sm:items-center sm:gap-x-2 sm:px-3"
                          >
                            <span
                              aria-hidden="true"
                              className="pt-1.5 text-center font-mono text-default-500 sm:pt-0"
                            >
                              {index + 1}
                            </span>
                            {guard ? (
                              <details className="group min-w-0">
                                <summary className="flex w-fit max-w-full cursor-pointer list-none items-center gap-1 rounded-md bg-success/10 px-1.5 py-1 font-medium text-success-700 dark:text-success-300 [&::-webkit-details-marker]:hidden">
                                  <ShieldCheck
                                    aria-hidden="true"
                                    className="shrink-0"
                                    size={13}
                                  />
                                  {guard}
                                  <ChevronDown
                                    aria-hidden="true"
                                    className="shrink-0 group-open:rotate-180"
                                    size={13}
                                  />
                                </summary>
                                <code className="mt-1 block whitespace-pre-wrap break-all rounded bg-default-100 px-2 py-1 font-mono text-xs text-default-700">
                                  {rule}
                                </code>
                              </details>
                            ) : (
                              <code
                                className="block min-w-0 truncate py-1 font-mono"
                                title={rule}
                              >
                                {rule}
                              </code>
                            )}
                            <div className="col-start-2 flex min-w-0 items-center gap-1 sm:col-start-auto">
                              {target && (
                                <Chip
                                  className="mr-auto max-w-full min-w-0 bg-default-100 px-1.5 text-default-600 sm:max-w-36"
                                  size="sm"
                                  title={target}
                                  variant="flat"
                                >
                                  <span className="block truncate">
                                    {target}
                                  </span>
                                </Chip>
                              )}
                              <div className="ml-auto flex shrink-0 gap-0.5">
                                <Button
                                  isIconOnly
                                  aria-label={`上移第 ${index + 1} 条规则`}
                                  className="size-7"
                                  isDisabled={index === 0}
                                  title="上移"
                                  variant="light"
                                  onPress={() => moveRule(index, -1)}
                                >
                                  <ArrowUp size={14} />
                                </Button>
                                <Button
                                  isIconOnly
                                  aria-label={`下移第 ${index + 1} 条规则`}
                                  className="size-7"
                                  isDisabled={index === ruleRows.length - 1}
                                  title="下移"
                                  variant="light"
                                  onPress={() => moveRule(index, 1)}
                                >
                                  <ArrowDown size={14} />
                                </Button>
                                <Button
                                  isIconOnly
                                  aria-label={`删除第 ${index + 1} 条规则`}
                                  className="size-7"
                                  color="danger"
                                  title="删除"
                                  variant="light"
                                  onPress={() => removeRule(lineIndex)}
                                >
                                  <Trash2 size={14} />
                                </Button>
                              </div>
                            </div>
                          </li>
                        );
                      })}
                    </ol>
                  )}
                </div>
              </Tab>

              <Tab
                key="advanced"
                title={
                  <span className="text-[10px] min-[360px]:text-xs sm:text-sm">
                    高级设置
                  </span>
                }
              >
                <div className="space-y-4 px-1 py-3 sm:px-2">
                  <div className="flex items-center justify-between gap-3 rounded-lg border border-divider px-3 py-2">
                    <div>
                      <label
                        className="text-sm font-medium"
                        htmlFor="generator-ipv4-setting"
                      >
                        仅 IPv4
                      </label>
                      <p
                        className="text-xs text-default-500"
                        id="generator-ipv4-description"
                      >
                        关闭导出配置中的 IPv6 和 DNS AAAA。使用 IPv6 节点或需要
                        TUN 接管 IPv6 时请保持关闭。
                      </p>
                    </div>
                    <Switch
                      aria-describedby="generator-ipv4-description"
                      id="generator-ipv4-setting"
                      isSelected={ipv4Only}
                      onValueChange={setIPv4Only}
                    />
                  </div>
                  <div className="space-y-2">
                    <Textarea
                      className="font-mono text-xs"
                      description="留空使用内置默认配置。代理节点、代理组和规则由订阅生成器写入。"
                      label="基础配置"
                      minRows={10}
                      placeholder={defaultBaseConfig}
                      value={baseConfig}
                      variant="bordered"
                      onChange={(event) => setBaseConfig(event.target.value)}
                    />
                    <Button
                      size="sm"
                      variant="flat"
                      onPress={() => setBaseConfig(defaultBaseConfig)}
                    >
                      填入默认配置
                    </Button>
                  </div>
                  <Textarea
                    className="font-mono text-xs"
                    description="可选 YAML 映射，配合 RULE-SET 规则使用。"
                    label="rule-providers"
                    minRows={6}
                    value={ruleProviders}
                    variant="bordered"
                    onChange={(event) => setRuleProviders(event.target.value)}
                  />
                </div>
              </Tab>
            </Tabs>
          </CardBody>
        </Card>
      </div>

      <Modal
        backdrop="blur"
        classNames={modalClassNames}
        isOpen={previewOpen}
        placement="center"
        scrollBehavior="inside"
        size="4xl"
        onOpenChange={setPreviewOpen}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader className="flex flex-col gap-1">
                <h2 className="text-xl font-bold">预览</h2>
                <p className="text-small text-default-500">
                  共 {effectiveNodes.length} 个节点
                </p>
              </ModalHeader>
              <ModalBody>
                {previewWarnings.length > 0 && (
                  <div className="rounded-lg border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-700 dark:bg-amber-950/50 dark:text-amber-100 space-y-1">
                    {previewWarnings.map((w, i) => (
                      <div key={i}>{w}</div>
                    ))}
                  </div>
                )}
                <pre className="text-xs font-mono whitespace-pre overflow-auto rounded-lg bg-default-100 p-3 max-h-[60vh]">
                  {previewContent}
                </pre>
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  关闭
                </Button>
                <Button
                  variant="flat"
                  onPress={() => copyText(previewContent, "配置")}
                >
                  复制
                </Button>
                <Button
                  color="primary"
                  variant="flat"
                  onPress={() =>
                    saveTextFile(
                      previewContent,
                      `${(fileName || name || "subscription").trim()}.yaml`,
                      "application/x-yaml;charset=utf-8",
                    )
                  }
                >
                  下载
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>
    </AnimatedPage>
  );
}
