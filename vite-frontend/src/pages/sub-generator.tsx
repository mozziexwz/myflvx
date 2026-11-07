import { useCallback, useEffect, useMemo, useState } from "react";
import toast from "react-hot-toast";
import { useNavigate, useSearchParams } from "react-router-dom";

import { AnimatedPage } from "@/components/animated-page";
import { PageLoadingState } from "@/components/page-state";
import { LabeledSwitch } from "@/components/labeled-switch";
import { SelectableCard } from "@/components/selectable-card";
import { Card, CardBody, CardHeader } from "@/shadcn-bridge/heroui/card";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Input, Textarea } from "@/shadcn-bridge/heroui/input";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import { Chip } from "@/shadcn-bridge/heroui/chip";
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
  getSubscriptionUsers,
  previewSubscription,
  updateSubscription,
  type GeneratorConfig,
  type ProxyGroupSpec,
  type ProxyNode,
  type SubscriptionTemplate,
  type UserBrief,
} from "@/api/enhanced";
import {
  copyText,
  hostPort,
  protocolLabel,
  saveTextFile,
} from "@/utils/enhanced";

// bridge Select 会把空 key 换成序号，“全部”选项用占位 key
const ALL_KEY = "__all__";
const fromFilterKey = (keys: Set<React.Key>) => {
  const value = String(Array.from(keys)[0] ?? ALL_KEY);

  return value === ALL_KEY ? "" : value;
};

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

const dateToMs = (value: string) => {
  if (!value) return 0;
  const ms = new Date(`${value}T23:59:59`).getTime();

  return Number.isNaN(ms) ? 0 : ms;
};

const msToDate = (ms: number) => {
  if (!ms) return "";
  const d = new Date(ms);
  const pad = (n: number) => String(n).padStart(2, "0");

  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
};

const cloneGroups = (groups: ProxyGroupSpec[]) =>
  groups.map((g) => ({ ...g, proxies: [...(g.proxies || [])] }));

export default function SubGeneratorPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const editId = Number(searchParams.get("id") || 0);

  const [loading, setLoading] = useState(true);
  const [nodes, setNodes] = useState<ProxyNode[]>([]);
  const [allTags, setAllTags] = useState<string[]>([]);
  const [templates, setTemplates] = useState<SubscriptionTemplate[]>([]);
  const [defaultBaseConfig, setDefaultBaseConfig] = useState("");
  const [users, setUsers] = useState<UserBrief[]>([]);

  const [name, setName] = useState("");
  const [fileName, setFileName] = useState("");
  const [remark, setRemark] = useState("");
  const [userIds, setUserIds] = useState<number[]>([]);
  const [enabled, setEnabled] = useState(true);
  const [expireDate, setExpireDate] = useState("");

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
  const [showAdvanced, setShowAdvanced] = useState(false);

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
    },
    [templates],
  );

  useEffect(() => {
    let cancelled = false;

    (async () => {
      setLoading(true);
      try {
        const [nodeRes, tplRes, userRes] = await Promise.all([
          getProxyNodes(),
          getSubscriptionTemplates(),
          getSubscriptionUsers(),
        ]);

        if (cancelled) return;
        const nodeList =
          nodeRes.code === 0 && nodeRes.data ? nodeRes.data.nodes : [];

        setNodes(nodeList);
        setAllTags(nodeRes.data?.tags || []);
        const tplList =
          tplRes.code === 0 && tplRes.data ? tplRes.data.templates : [];

        setTemplates(tplList);
        setDefaultBaseConfig(tplRes.data?.defaultBaseConfig || "");
        if (userRes.code === 0 && userRes.data) setUsers(userRes.data);

        if (editId > 0) {
          const subRes = await getSubscription(editId);

          if (cancelled) return;
          if (subRes.code !== 0 || !subRes.data) {
            toast.error(subRes.msg || "订阅不存在");

            return;
          }
          const s = subRes.data;
          const cfg = s.genConfig;

          setName(s.name);
          setFileName(s.fileName?.replace(/\.(yaml|yml|txt)$/i, "") || "");
          setRemark(s.remark || "");
          setUserIds(s.userIds || []);
          setEnabled(s.enabled);
          setExpireDate(msToDate(s.expireTime));
          if (cfg) {
            setAllNodes(Boolean(cfg.allNodes));
            setTags(cfg.tags || []);
            setSelectedNodeIds(cfg.nodeIds || []);
            setTemplateId(cfg.template || "standard");
            const tpl = tplList.find(
              (t) => t.id === (cfg.template || "standard"),
            );

            setGroups(
              cfg.groups && cfg.groups.length > 0
                ? cloneGroups(cfg.groups)
                : cloneGroups(tpl?.groups || []),
            );
            setRulesText(
              (cfg.rules && cfg.rules.length > 0
                ? cfg.rules
                : tpl?.rules || []
              ).join("\n"),
            );
            setBaseConfig(cfg.baseConfig || "");
            setRuleProviders(cfg.ruleProviders || "");
            if (cfg.baseConfig || cfg.ruleProviders) setShowAdvanced(true);
          }
        } else {
          const tpl = tplList.find((t) => t.id === "standard") || tplList[0];

          if (tpl) {
            setTemplateId(tpl.id);
            setGroups(cloneGroups(tpl.groups));
            setRulesText(tpl.rules.join("\n"));
          }
          setSelectedNodeIds(
            nodeList.filter((n) => n.enabled).map((n) => n.id),
          );
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [editId]);

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

    return selectedNodeIds
      .map((id) => nodes.find((n) => n.id === id))
      .filter((n): n is ProxyNode => Boolean(n && n.enabled));
  }, [allNodes, enabledNodes, tags, selectedNodeIds, nodes]);

  const toggleNode = (id: number) => {
    setSelectedNodeIds((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id],
    );
  };

  const selectVisible = (select: boolean) => {
    const ids = visibleNodes.map((n) => n.id);

    setSelectedNodeIds((prev) =>
      select
        ? [...prev, ...ids.filter((id) => !prev.includes(id))]
        : prev.filter((id) => !ids.includes(id)),
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
    setGroups((prev) => {
      const target = index + delta;

      if (target < 0 || target >= prev.length) return prev;
      const next = [...prev];
      const [item] = next.splice(index, 1);

      next.splice(target, 0, item);

      return next;
    });
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
        userIds,
        enabled,
        expireTime: dateToMs(expireDate),
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

  const assignableUsers = users.filter((u) => u.roleId !== 0);

  return (
    <AnimatedPage className="px-3 lg:px-6 py-8 space-y-4">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-bold text-foreground">
            {editId > 0 ? "编辑生成订阅" : "生成订阅"}
          </h1>
          <p className="text-sm text-default-500">
            从节点管理中的节点生成 Clash / mihomo
            订阅；用户每次下载都会按最新节点重新生成。
          </p>
        </div>
        <div className="flex gap-2">
          <Button
            size="sm"
            variant="light"
            onPress={() => navigate("/subscription")}
          >
            返回
          </Button>
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

      <Card>
        <CardHeader>
          <h2 className="font-semibold">基本信息</h2>
        </CardHeader>
        <CardBody className="space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <Input
              isRequired
              label="订阅名称"
              value={name}
              variant="bordered"
              onChange={(e) => setName(e.target.value)}
            />
            <Input
              description="下载时的文件名，默认使用订阅名称"
              label="文件名（可选）"
              value={fileName}
              variant="bordered"
              onChange={(e) => setFileName(e.target.value)}
            />
          </div>
          <Select
            description="被分配的用户可以在「订阅下载」页下载；管理员始终可以下载"
            label="分配给用户"
            placeholder="不分配"
            selectedKeys={userIds.map(String)}
            selectionMode="multiple"
            onSelectionChange={(keys) =>
              setUserIds(Array.from(keys).map((k) => Number(k)))
            }
          >
            {assignableUsers.map((u) => (
              <SelectItem key={String(u.id)}>
                {u.status === 1 ? u.user : `${u.user}（已禁用）`}
              </SelectItem>
            ))}
          </Select>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <Input
              description="到期后用户不能再下载，留空不限制"
              label="下载截止日期（可选）"
              type="date"
              value={expireDate}
              variant="bordered"
              onChange={(e) => setExpireDate(e.target.value)}
            />
            <Input
              label="备注"
              value={remark}
              variant="bordered"
              onChange={(e) => setRemark(e.target.value)}
            />
          </div>
          <LabeledSwitch
            isSelected={enabled}
            label="启用"
            onValueChange={setEnabled}
          />
        </CardBody>
      </Card>

      <Card>
        <CardHeader className="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
          <h2 className="font-semibold">
            选择节点
            <span className="ml-2 text-sm font-normal text-default-500">
              将包含 {effectiveNodes.length} 个节点
            </span>
          </h2>
          <div className="flex gap-2">
            <Button
              color={!allNodes ? "primary" : "default"}
              size="sm"
              variant="flat"
              onPress={() => setAllNodes(false)}
            >
              手动选择
            </Button>
            <Button
              color={allNodes ? "primary" : "default"}
              size="sm"
              variant="flat"
              onPress={() => setAllNodes(true)}
            >
              全部启用节点
            </Button>
          </div>
        </CardHeader>
        <CardBody>
          {allNodes ? (
            <Select
              description="留空表示所有启用的节点；新增的节点会自动加入订阅"
              label="按标签筛选（可选）"
              placeholder="全部标签"
              selectedKeys={tags}
              selectionMode="multiple"
              onSelectionChange={(keys) =>
                setTags(Array.from(keys).map(String))
              }
            >
              {allTags.map((t) => (
                <SelectItem key={t}>{t}</SelectItem>
              ))}
            </Select>
          ) : nodes.length === 0 ? (
            <div className="text-sm text-default-500 py-6 text-center">
              还没有节点，请先到
              <button
                className="text-primary mx-1"
                type="button"
                onClick={() => navigate("/proxy-node")}
              >
                节点管理
              </button>
              添加或导入节点。
            </div>
          ) : (
            <div className="space-y-3">
              <div className="flex flex-wrap items-center gap-2">
                <div className="w-56">
                  <Input
                    placeholder="搜索节点"
                    size="sm"
                    value={nodeKeyword}
                    variant="bordered"
                    onChange={(e) => setNodeKeyword(e.target.value)}
                  />
                </div>
                <div className="w-36">
                  <Select
                    aria-label="标签"
                    placeholder="全部标签"
                    selectedKeys={[nodeTagFilter || ALL_KEY]}
                    size="sm"
                    onSelectionChange={(keys) =>
                      setNodeTagFilter(fromFilterKey(keys))
                    }
                  >
                    {[
                      { key: ALL_KEY, label: "全部标签" },
                      ...allTags.map((t) => ({ key: t, label: t })),
                    ].map((item) => (
                      <SelectItem key={item.key}>{item.label}</SelectItem>
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
              <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-2 max-h-[360px] overflow-auto pr-1">
                {visibleNodes.map((n) => (
                  <SelectableCard
                    key={n.id}
                    className={`items-center ${n.enabled ? "" : "opacity-50"}`}
                    selected={selectedSet.has(n.id)}
                    onToggle={() => toggleNode(n.id)}
                  >
                    <Checkbox
                      className="pointer-events-none"
                      isSelected={selectedSet.has(n.id)}
                    />
                    <div className="min-w-0 flex-1">
                      <div className="text-sm truncate">{n.name}</div>
                      <div className="text-xs text-default-500 font-mono truncate">
                        {protocolLabel(n.protocol)} ·{" "}
                        {hostPort(n.server, n.port)}
                        {!n.enabled && "（已停用）"}
                      </div>
                    </div>
                  </SelectableCard>
                ))}
              </div>
            </div>
          )}
        </CardBody>
      </Card>

      <Card>
        <CardHeader className="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
          <h2 className="font-semibold">代理组</h2>
          <div className="flex flex-wrap items-center gap-2">
            <div className="w-40">
              <Select
                aria-label="模板"
                selectedKeys={[templateId]}
                size="sm"
                onSelectionChange={(keys) =>
                  setTemplateId(String(Array.from(keys)[0] ?? "standard"))
                }
              >
                {templates.map((t) => (
                  <SelectItem key={t.id}>{t.name}</SelectItem>
                ))}
              </Select>
            </div>
            <Button
              size="sm"
              variant="flat"
              onPress={() => applyTemplate(templateId)}
            >
              套用模板
            </Button>
            <Button color="primary" size="sm" variant="flat" onPress={addGroup}>
              添加代理组
            </Button>
          </div>
        </CardHeader>
        <CardBody className="space-y-3">
          <p className="text-xs text-default-500">
            {templates.find((t) => t.id === templateId)?.description}
            。套用模板会替换下面的代理组和规则；勾选「包含节点」的组会自动加入所选节点（可用正则筛选，例如
            <code className="mx-1">香港|HK</code>）。
          </p>
          {groups.map((g, index) => {
            const options = memberOptions(g.name);
            const isAuto =
              g.type === "url-test" ||
              g.type === "fallback" ||
              g.type === "load-balance";

            return (
              <div
                key={index}
                className="rounded-lg border border-divider p-3 space-y-3"
              >
                <div className="flex flex-col md:flex-row gap-3">
                  <div className="md:w-56">
                    <Input
                      label="名称"
                      size="sm"
                      value={g.name}
                      variant="bordered"
                      onChange={(e) => renameGroup(index, e.target.value)}
                    />
                  </div>
                  <div className="md:w-56">
                    <Select
                      label="类型"
                      selectedKeys={[g.type]}
                      size="sm"
                      onSelectionChange={(keys) =>
                        updateGroup(index, {
                          type: String(Array.from(keys)[0] ?? "select"),
                        })
                      }
                    >
                      {GROUP_TYPES.map((t) => (
                        <SelectItem key={t.key}>{t.label}</SelectItem>
                      ))}
                    </Select>
                  </div>
                  <div className="flex-1">
                    <Select
                      label="成员（代理组 / 策略 / 节点）"
                      placeholder="未选择"
                      selectedKeys={g.proxies.filter((m) =>
                        options.some((o) => o.key === m),
                      )}
                      selectionMode="multiple"
                      size="sm"
                      onSelectionChange={(keys) =>
                        updateGroup(index, {
                          proxies: (() => {
                            const chosen = Array.from(keys).map(String);
                            const kept = g.proxies.filter((m) =>
                              chosen.includes(m),
                            );

                            return [
                              ...kept,
                              ...chosen.filter((m) => !kept.includes(m)),
                            ];
                          })(),
                        })
                      }
                    >
                      {options.map((o) => (
                        <SelectItem key={o.key}>{o.label}</SelectItem>
                      ))}
                    </Select>
                  </div>
                </div>
                <div className="flex flex-col md:flex-row md:items-center gap-3">
                  <Checkbox
                    isSelected={g.includeAllNodes}
                    onValueChange={(v) =>
                      updateGroup(index, { includeAllNodes: v })
                    }
                  >
                    包含节点
                  </Checkbox>
                  {g.includeAllNodes && (
                    <div className="md:w-64">
                      <Input
                        placeholder="节点筛选正则（可选）"
                        size="sm"
                        value={g.filter || ""}
                        variant="bordered"
                        onChange={(e) =>
                          updateGroup(index, { filter: e.target.value })
                        }
                      />
                    </div>
                  )}
                  {isAuto && (
                    <>
                      <div className="md:w-72">
                        <Input
                          placeholder="测速地址（默认 gstatic generate_204）"
                          size="sm"
                          value={g.url || ""}
                          variant="bordered"
                          onChange={(e) =>
                            updateGroup(index, { url: e.target.value })
                          }
                        />
                      </div>
                      <div className="md:w-32">
                        <Input
                          placeholder="间隔(秒) 300"
                          size="sm"
                          type="number"
                          value={g.interval ? String(g.interval) : ""}
                          variant="bordered"
                          onChange={(e) =>
                            updateGroup(index, {
                              interval: Number(e.target.value) || undefined,
                            })
                          }
                        />
                      </div>
                    </>
                  )}
                  <div className="flex gap-1 md:ml-auto">
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
                {g.proxies.length > 0 && (
                  <div className="flex flex-wrap gap-1">
                    {g.proxies.map((m) => (
                      <Chip key={m} size="sm" variant="flat">
                        {m}
                      </Chip>
                    ))}
                    {g.includeAllNodes && (
                      <Chip color="primary" size="sm" variant="flat">
                        + 节点{g.filter ? `（${g.filter}）` : ""}
                      </Chip>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </CardBody>
      </Card>

      <Card>
        <CardHeader>
          <h2 className="font-semibold">规则</h2>
        </CardHeader>
        <CardBody className="space-y-2">
          <Textarea
            className="font-mono"
            description="每行一条 mihomo 规则，例如 DOMAIN-SUFFIX,example.com,🚀 节点选择；目标不存在会自动改为第一个代理组，末尾自动补 MATCH"
            minRows={10}
            value={rulesText}
            variant="bordered"
            onChange={(e) => setRulesText(e.target.value)}
          />
        </CardBody>
      </Card>

      <Card>
        <CardHeader className="flex items-center justify-between">
          <h2 className="font-semibold">高级设置</h2>
          <Button
            size="sm"
            variant="light"
            onPress={() => setShowAdvanced((v) => !v)}
          >
            {showAdvanced ? "收起" : "展开"}
          </Button>
        </CardHeader>
        {showAdvanced && (
          <CardBody className="space-y-4">
            <Textarea
              className="font-mono"
              description="端口、DNS、嗅探等全局配置（YAML）。留空使用内置默认配置；proxies / proxy-groups / rules 会被自动覆盖"
              label="基础配置"
              minRows={10}
              placeholder={defaultBaseConfig}
              value={baseConfig}
              variant="bordered"
              onChange={(e) => setBaseConfig(e.target.value)}
            />
            <div className="flex gap-2">
              <Button
                size="sm"
                variant="flat"
                onPress={() => setBaseConfig(defaultBaseConfig)}
              >
                填入默认配置
              </Button>
            </div>
            <Textarea
              className="font-mono"
              description="可选，rule-providers 映射（YAML），配合 RULE-SET 规则使用"
              label="rule-providers"
              minRows={6}
              value={ruleProviders}
              variant="bordered"
              onChange={(e) => setRuleProviders(e.target.value)}
            />
          </CardBody>
        )}
      </Card>

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
