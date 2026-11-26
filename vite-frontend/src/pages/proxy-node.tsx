import type { UserTunnelApiItem } from "@/api/types";
import type { DragEndEvent } from "@dnd-kit/core";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import toast from "react-hot-toast";
import { useNavigate } from "react-router-dom";
import { DndContext, closestCenter } from "@dnd-kit/core";
import {
  SortableContext,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { ArrowRight, CornerDownRight, Ellipsis, Search } from "lucide-react";

import { AnimatedPage } from "@/components/animated-page";
import {
  DragHandle,
  SortableTableRow,
  applyOrder,
  draggedIds,
  orderItems,
  reorderIds,
  useSortSensors,
} from "@/components/sortable";
import { PageLoadingState } from "@/components/page-state";
import { PageIntro } from "@/components/page-intro";
import { LabeledSwitch } from "@/components/labeled-switch";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { Card, CardBody } from "@/shadcn-bridge/heroui/card";
import { Button } from "@/shadcn-bridge/heroui/button";
import {
  Dropdown,
  DropdownMenu,
  DropdownTrigger,
} from "@/shadcn-bridge/heroui/dropdown";
import { Input, Textarea } from "@/shadcn-bridge/heroui/input";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";
import { Switch } from "@/shadcn-bridge/heroui/switch";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Tabs, Tab } from "@/shadcn-bridge/heroui/tabs";
import {
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
} from "@/shadcn-bridge/heroui/modal";
import {
  Table,
  TableHeader,
  TableColumn,
  TableBody,
  TableCell,
} from "@/shadcn-bridge/heroui/table";
import { userTunnel } from "@/api";
import {
  bindRelay,
  createProxyNode,
  createRelay,
  deleteProxyNodes,
  getProxyNodes,
  getProxyShareLinks,
  getRelayCandidates,
  importProxyNodes,
  parseProxyContent,
  setProxyNodeTags,
  toggleProxyNodes,
  updateProxyNode,
  updateProxyNodeOrder,
  type ParsedProxyBrief,
  type ProxyNode,
  type RelayCandidate,
} from "@/api/enhanced";
import { useLocalStorageState } from "@/hooks/use-local-storage-state";
import { cn } from "@/lib/utils";
import { isAdmin } from "@/utils/auth";
import {
  copyText,
  firstKey,
  hostPort,
  protocolLabel,
  sourceLabel,
  splitInputTags,
} from "@/utils/enhanced";

const SOURCE_OPTIONS = [
  { key: "manual", label: "手动" },
  { key: "import", label: "导入" },
  { key: "deploy", label: "部署" },
  { key: "relay", label: "中转" },
];

const modalClassNames = {
  base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden",
};

interface EditForm {
  id?: number;
  name: string;
  link: string;
  configText: string;
  tags: string;
  remark: string;
  enabled: boolean;
  relayHost: string;
  isRelay: boolean;
}

const emptyEditForm: EditForm = {
  name: "",
  link: "",
  configText: "",
  tags: "",
  remark: "",
  enabled: true,
  relayHost: "",
  isRelay: false,
};

export default function ProxyNodePage() {
  const navigate = useNavigate();
  const [loading, setLoading] = useState(true);
  const [nodes, setNodes] = useState<ProxyNode[]>([]);
  const [allTags, setAllTags] = useState<string[]>([]);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [keyword, setKeyword] = useLocalStorageState("proxy-node-keyword", "");
  const [tagFilter, setTagFilter] = useLocalStorageState("proxy-node-tag", "");
  const [sourceFilter, setSourceFilter] = useLocalStorageState(
    "proxy-node-source",
    "",
  );
  const [tunnelFilter, setTunnelFilter] = useLocalStorageState(
    "proxy-node-tunnel",
    "",
  );
  const admin = isAdmin();

  const [editOpen, setEditOpen] = useState(false);
  const [editForm, setEditForm] = useState<EditForm>(emptyEditForm);
  const [editMode, setEditMode] = useState<"link" | "config">("link");
  const [saving, setSaving] = useState(false);

  const [importOpen, setImportOpen] = useState(false);
  const [importContent, setImportContent] = useState("");
  const [importUrl, setImportUrl] = useState("");
  const [importUA, setImportUA] = useState("");
  const [importTags, setImportTags] = useState("");
  const [importSkipDup, setImportSkipDup] = useState(true);
  const [importPreview, setImportPreview] = useState<{
    proxies: ParsedProxyBrief[];
    failed: string[];
  } | null>(null);
  const [importing, setImporting] = useState(false);

  const [tagsOpen, setTagsOpen] = useState(false);
  const [batchTags, setBatchTags] = useState("");

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteIds, setDeleteIds] = useState<number[]>([]);
  const [deleteForwards, setDeleteForwards] = useState(true);
  const [deleting, setDeleting] = useState(false);

  const [relayOpen, setRelayOpen] = useState(false);
  const [relayNode, setRelayNode] = useState<ProxyNode | null>(null);
  const [relayTab, setRelayTab] = useState<string>("create");
  const [tunnels, setTunnels] = useState<UserTunnelApiItem[]>([]);
  const [relayTunnelId, setRelayTunnelId] = useState("");
  const [relayPort, setRelayPort] = useState("");
  const [relayHost, setRelayHost] = useState("");
  const [relayName, setRelayName] = useState("");
  const [relayAllEntries, setRelayAllEntries] = useState(false);
  const [relayCandidates, setRelayCandidates] = useState<RelayCandidate[]>([]);
  const [relaySubmitting, setRelaySubmitting] = useState(false);

  const loadNodes = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    try {
      const res = await getProxyNodes();

      if (res.code === 0 && res.data) {
        setNodes(res.data.nodes || []);
        setAllTags(res.data.tags || []);
      } else {
        toast.error(res.msg || "加载节点失败");
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadNodes();
  }, [loadNodes]);

  // 隧道筛选的选项来自中转节点所用的隧道
  const tunnelOptions = useMemo(() => {
    const map = new Map<string, string>();

    nodes.forEach((n) => {
      if (n.tunnelId > 0) {
        map.set(String(n.tunnelId), n.tunnelName || `隧道 #${n.tunnelId}`);
      }
    });

    return Array.from(map, ([key, label]) => ({ key, label }));
  }, [nodes]);

  useEffect(() => {
    if (
      !loading &&
      tunnelFilter &&
      !tunnelOptions.some((t) => t.key === tunnelFilter)
    ) {
      setTunnelFilter("");
    }
  }, [loading, tunnelFilter, tunnelOptions, setTunnelFilter]);

  const filteredNodes = useMemo(() => {
    const kw = keyword.trim().toLowerCase();

    return nodes.filter((n) => {
      if (tagFilter && !n.tags.includes(tagFilter)) return false;
      if (sourceFilter && n.source !== sourceFilter) return false;
      if (tunnelFilter && String(n.tunnelId) !== tunnelFilter) return false;
      if (!kw) return true;

      return (
        n.name.toLowerCase().includes(kw) ||
        n.server.toLowerCase().includes(kw) ||
        n.protocol.toLowerCase().includes(kw) ||
        (n.remark || "").toLowerCase().includes(kw)
      );
    });
  }, [nodes, keyword, tagFilter, sourceFilter, tunnelFilter]);

  const {
    displayNodes,
    rootIds,
    relaysByParent,
    relayParents,
    lastRelayIds,
    relayCounts,
  } = useMemo(() => {
    const visibleSourceIds = new Set(
      filteredNodes
        .filter((node) => node.source !== "relay")
        .map((node) => node.id),
    );
    const roots: ProxyNode[] = [];
    const relaysByParent = new Map<number, ProxyNode[]>();
    const relayParents = new Map<number, number>();

    for (const node of filteredNodes) {
      if (node.source === "relay" && visibleSourceIds.has(node.parentId)) {
        const relays = relaysByParent.get(node.parentId) ?? [];

        relays.push(node);
        relaysByParent.set(node.parentId, relays);
        relayParents.set(node.id, node.parentId);
      } else {
        roots.push(node);
      }
    }

    const lastRelayIds = new Set<number>();
    const relayCounts = new Map<number, number>();

    for (const [parentId, relays] of relaysByParent) {
      lastRelayIds.add(relays[relays.length - 1].id);
      relayCounts.set(parentId, relays.length);
    }

    return {
      displayNodes: roots.flatMap((node) => [
        node,
        ...(relaysByParent.get(node.id) ?? []),
      ]),
      rootIds: roots.map((node) => node.id),
      relaysByParent,
      relayParents,
      lastRelayIds,
      relayCounts,
    };
  }, [filteredNodes]);

  const allFilteredSelected =
    filteredNodes.length > 0 && filteredNodes.every((n) => selected.has(n.id));

  const toggleSelectAll = () => {
    setSelected((prev) => {
      const next = new Set(prev);

      if (allFilteredSelected) {
        filteredNodes.forEach((n) => next.delete(n.id));
      } else {
        filteredNodes.forEach((n) => next.add(n.id));
      }

      return next;
    });
  };

  const toggleSelect = (id: number) => {
    setSelected((prev) => {
      const next = new Set(prev);

      if (next.has(id)) next.delete(id);
      else next.add(id);

      return next;
    });
  };

  // ─── 新增 / 编辑 ───────────────────────────────────────────

  const openCreate = () => {
    setEditForm(emptyEditForm);
    setEditMode("link");
    setEditOpen(true);
  };

  const openEdit = (node: ProxyNode) => {
    const config = { ...node.config };

    setEditForm({
      id: node.id,
      name: node.name,
      link: "",
      configText: JSON.stringify(config, null, 2),
      tags: node.tags.join(", "),
      remark: node.remark || "",
      enabled: node.enabled,
      relayHost: node.relayHost || "",
      isRelay: node.source === "relay",
    });
    setEditMode("config");
    setEditOpen(true);
  };

  const submitEdit = async () => {
    const payload: Parameters<typeof updateProxyNode>[0] = {
      id: editForm.id,
      name: editForm.name.trim(),
      tags: splitInputTags(editForm.tags),
      remark: editForm.remark,
      enabled: editForm.enabled,
    };

    if (editForm.isRelay) {
      payload.relayHost = editForm.relayHost.trim();
    } else if (editMode === "link") {
      if (!editForm.link.trim() && !editForm.id) {
        toast.error("请粘贴分享链接");

        return;
      }
      if (editForm.link.trim()) payload.link = editForm.link.trim();
    } else {
      try {
        const parsed = JSON.parse(editForm.configText || "{}");

        if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
          throw new Error("invalid");
        }
        payload.config = parsed;
      } catch {
        toast.error("节点配置必须是 JSON 对象（Clash 代理字段）");

        return;
      }
    }

    setSaving(true);
    try {
      const res = editForm.id
        ? await updateProxyNode(payload)
        : await createProxyNode(payload);

      if (res.code === 0) {
        const warnings =
          editForm.id && res.data && "warnings" in (res.data as object)
            ? ((res.data as { warnings?: string[] }).warnings ?? [])
            : [];

        toast.success(editForm.id ? "已保存" : "已添加");
        warnings.forEach((w) => toast(w, { icon: "⚠️", duration: 6000 }));
        setEditOpen(false);
        void loadNodes(true);
      } else {
        toast.error(res.msg || "保存失败");
      }
    } finally {
      setSaving(false);
    }
  };

  // ─── 导入 ─────────────────────────────────────────────────

  const openImport = () => {
    setImportContent("");
    setImportUrl("");
    setImportUA("");
    setImportTags("");
    setImportPreview(null);
    setImportOpen(true);
  };

  const importPayload = () => ({
    content: importContent,
    url: importUrl.trim(),
    userAgent: importUA.trim(),
    tags: splitInputTags(importTags),
    skipDuplicates: importSkipDup,
  });

  const previewImport = async () => {
    setImporting(true);
    try {
      const res = await parseProxyContent(importPayload());

      if (res.code === 0 && res.data) {
        setImportPreview({
          proxies: res.data.proxies || [],
          failed: res.data.failed || [],
        });
      } else {
        toast.error(res.msg || "解析失败");
      }
    } finally {
      setImporting(false);
    }
  };

  const submitImport = async () => {
    setImporting(true);
    try {
      const res = await importProxyNodes(importPayload());

      if (res.code === 0 && res.data) {
        const { created, skipped, failed } = res.data;

        toast.success(
          `导入 ${created} 个节点${skipped ? `，跳过重复 ${skipped} 个` : ""}${
            failed?.length ? `，${failed.length} 行无法识别` : ""
          }`,
        );
        setImportOpen(false);
        void loadNodes(true);
      } else {
        toast.error(res.msg || "导入失败");
      }
    } finally {
      setImporting(false);
    }
  };

  // ─── 拖拽排序 ─────────────────────────────────────────────

  const sortSensors = useSortSensors();
  const sorting = useRef(false);

  const handleDragEnd = async (event: DragEndEvent) => {
    const ids = draggedIds(event);

    if (!ids || sorting.current) return;
    const [activeId, overId] = ids;
    const activeParent = relayParents.get(activeId);
    const overParent = relayParents.get(overId);
    // 源节点按组移动，组内的中转节点可单独调整顺序。
    const sortableIds =
      activeParent === undefined
        ? rootIds
        : (relaysByParent.get(activeParent) ?? []).map((node) => node.id);
    const previousOrder = nodes.map((node) => node.id);
    const order = reorderIds(
      previousOrder,
      sortableIds,
      activeId,
      activeParent === undefined ? (overParent ?? overId) : overId,
    );

    if (!order) return;
    sorting.current = true;
    setNodes(applyOrder(nodes, order));
    try {
      const res = await updateProxyNodeOrder(orderItems(order));

      if (res.code !== 0) {
        toast.error(res.msg || "保存排序失败");
        const previousIds = new Set(previousOrder);

        setNodes((current) =>
          applyOrder(current, [
            ...previousOrder,
            ...current
              .filter((node) => !previousIds.has(node.id))
              .map((node) => node.id),
          ]),
        );
      }
    } finally {
      sorting.current = false;
    }
  };

  // ─── 批量操作 ─────────────────────────────────────────────

  const selectedIds = Array.from(selected);

  const batchToggle = async (enabled: boolean) => {
    if (selectedIds.length === 0) return;
    const res = await toggleProxyNodes(selectedIds, enabled);

    if (res.code === 0) {
      toast.success(enabled ? "已启用" : "已停用");
      void loadNodes(true);
    } else {
      toast.error(res.msg || "操作失败");
    }
  };

  const toggleOne = async (node: ProxyNode, enabled: boolean) => {
    const res = await toggleProxyNodes([node.id], enabled);

    if (res.code === 0) {
      setNodes((prev) =>
        prev.map((n) => (n.id === node.id ? { ...n, enabled } : n)),
      );
    } else {
      toast.error(res.msg || "操作失败");
    }
  };

  const copyLinks = async (ids: number[]) => {
    const res = await getProxyShareLinks(ids);

    if (res.code === 0 && res.data?.links) {
      await copyText(res.data.links, `${ids.length} 条分享链接`);
    } else {
      toast.error(res.msg || "没有可导出的分享链接");
    }
  };

  const submitBatchTags = async () => {
    const res = await setProxyNodeTags(selectedIds, splitInputTags(batchTags));

    if (res.code === 0) {
      toast.success("标签已更新");
      setTagsOpen(false);
      void loadNodes(true);
    } else {
      toast.error(res.msg || "操作失败");
    }
  };

  const openDelete = (ids: number[]) => {
    setDeleteIds(ids);
    setDeleteForwards(true);
    setDeleteOpen(true);
  };

  const deleteHasRelay = useMemo(() => {
    const ids = new Set(deleteIds);

    return nodes.some(
      (n) =>
        (ids.has(n.id) && n.source === "relay") ||
        (n.source === "relay" && ids.has(n.parentId)),
    );
  }, [deleteIds, nodes]);

  const confirmDelete = async () => {
    setDeleting(true);
    try {
      const res = await deleteProxyNodes(deleteIds, deleteForwards);

      if (res.code === 0) {
        toast.success(`已删除 ${res.data?.deleted ?? deleteIds.length} 个节点`);
        (res.data?.warnings || []).forEach((w) =>
          toast(w, { icon: "⚠️", duration: 6000 }),
        );
        setDeleteOpen(false);
        setSelected(new Set());
        void loadNodes(true);
      } else {
        toast.error(res.msg || "删除失败");
      }
    } finally {
      setDeleting(false);
    }
  };

  // ─── 隧道中转 ─────────────────────────────────────────────

  const openRelay = async (node: ProxyNode) => {
    setRelayNode(node);
    setRelayTab("create");
    setRelayTunnelId("");
    setRelayPort("");
    setRelayHost("");
    setRelayName("");
    setRelayAllEntries(false);
    setRelayCandidates([]);
    setRelayOpen(true);
    // 只列出当前用户有权限的隧道（管理员为全部启用的隧道）
    const [tunnelRes, candidateRes] = await Promise.all([
      userTunnel(),
      getRelayCandidates(node.id),
    ]);

    if (tunnelRes.code === 0 && tunnelRes.data) {
      setTunnels(tunnelRes.data);
    }
    if (candidateRes.code === 0 && candidateRes.data) {
      setRelayCandidates(candidateRes.data);
    }
  };

  const submitRelay = async () => {
    if (!relayNode) return;
    if (!relayTunnelId) {
      toast.error("请选择隧道");

      return;
    }
    setRelaySubmitting(true);
    try {
      const res = await createRelay({
        nodeId: relayNode.id,
        tunnelId: Number(relayTunnelId),
        inPort: relayPort ? Number(relayPort) : undefined,
        relayHost: relayHost.trim(),
        name: relayName.trim(),
        allEntries: relayAllEntries,
      });

      if (res.code === 0 && res.data) {
        toast.success(`已创建转发并生成 ${res.data.nodes.length} 个中转节点`);
        setRelayOpen(false);
        void loadNodes(true);
      } else {
        toast.error(res.msg || "创建失败");
      }
    } finally {
      setRelaySubmitting(false);
    }
  };

  const bindCandidate = async (candidate: RelayCandidate) => {
    if (!relayNode) return;
    setRelaySubmitting(true);
    try {
      const res = await bindRelay({
        nodeId: relayNode.id,
        forwardId: candidate.id,
        relayHost: relayHost.trim(),
        name: relayName.trim(),
        allEntries: relayAllEntries,
      });

      if (res.code === 0 && res.data) {
        toast.success(`已生成 ${res.data.nodes.length} 个中转节点`);
        setRelayOpen(false);
        void loadNodes(true);
      } else {
        toast.error(res.msg || "生成失败");
      }
    } finally {
      setRelaySubmitting(false);
    }
  };

  if (loading) {
    return <PageLoadingState message="正在加载节点..." />;
  }

  const hasFilter = Boolean(
    keyword.trim() || tagFilter || sourceFilter || tunnelFilter,
  );

  return (
    <AnimatedPage className="px-3 lg:px-6 py-8">
      <PageIntro
        description="中转节点按源节点归组。勾选节点可直接生成订阅。"
        title="代理节点"
      />
      <div className="flex flex-col lg:flex-row items-stretch lg:items-center justify-between mb-4 gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <Input
            aria-label="搜索代理节点"
            classNames={{ base: "w-full sm:w-56", inputWrapper: "bg-card" }}
            placeholder="搜索名称 / 地址 / 协议"
            size="sm"
            startContent={<Search aria-hidden="true" size={14} />}
            type="search"
            value={keyword}
            variant="bordered"
            onChange={(event) => setKeyword(event.target.value)}
          />
          <div className="w-28 [&_label]:sr-only">
            <Select
              label="标签筛选"
              placeholder="全部标签"
              selectedKeys={tagFilter ? [tagFilter] : []}
              size="sm"
              onSelectionChange={(keys) => setTagFilter(firstKey(keys))}
            >
              {allTags.map((t) => (
                <SelectItem key={t}>{t}</SelectItem>
              ))}
            </Select>
          </div>
          <div className="w-28 [&_label]:sr-only">
            <Select
              label="来源筛选"
              placeholder="全部来源"
              selectedKeys={sourceFilter ? [sourceFilter] : []}
              size="sm"
              onSelectionChange={(keys) => setSourceFilter(firstKey(keys))}
            >
              {SOURCE_OPTIONS.map((item) => (
                <SelectItem key={item.key}>{item.label}</SelectItem>
              ))}
            </Select>
          </div>
          <div className="w-28 [&_label]:sr-only">
            <Select
              label="隧道筛选"
              placeholder="全部隧道"
              selectedKeys={tunnelFilter ? [tunnelFilter] : []}
              size="sm"
              onSelectionChange={(keys) => setTunnelFilter(firstKey(keys))}
            >
              {tunnelOptions.map((item) => (
                <SelectItem key={item.key}>{item.label}</SelectItem>
              ))}
            </Select>
          </div>
          {hasFilter && (
            <Button
              size="sm"
              variant="light"
              onPress={() => {
                setKeyword("");
                setTagFilter("");
                setSourceFilter("");
                setTunnelFilter("");
              }}
            >
              清除筛选
            </Button>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {admin && (
            <Button
              size="sm"
              variant="flat"
              onPress={() => navigate("/deploy")}
            >
              部署节点
            </Button>
          )}
          <Button size="sm" variant="flat" onPress={openImport}>
            导入
          </Button>
          <Button color="primary" size="sm" variant="flat" onPress={openCreate}>
            新增
          </Button>
        </div>
      </div>

      {selected.size > 0 && (
        <div className="mb-4 flex flex-col gap-2 rounded-xl border border-primary-200 bg-primary-50/60 px-4 py-2.5 sm:flex-row sm:items-center sm:justify-between dark:border-primary-800 dark:bg-primary-900/20">
          <span className="shrink-0 text-sm font-medium text-foreground">
            已选择 {selected.size} 个节点
          </span>
          <div className="flex flex-wrap items-center gap-2">
            <Button
              color="primary"
              endContent={<ArrowRight aria-hidden="true" size={14} />}
              size="sm"
              onPress={() =>
                navigate(`/sub-generator?nodes=${selectedIds.join(",")}`)
              }
            >
              用所选生成订阅
            </Button>
            <Button size="sm" variant="flat" onPress={() => batchToggle(true)}>
              启用
            </Button>
            <Button size="sm" variant="flat" onPress={() => batchToggle(false)}>
              停用
            </Button>
            <Button
              size="sm"
              variant="flat"
              onPress={() => {
                setBatchTags("");
                setTagsOpen(true);
              }}
            >
              设置标签
            </Button>
            <Button
              size="sm"
              variant="flat"
              onPress={() => copyLinks(selectedIds)}
            >
              复制分享链接
            </Button>
            <Button
              color="danger"
              size="sm"
              variant="flat"
              onPress={() => openDelete(selectedIds)}
            >
              删除
            </Button>
            <Button
              size="sm"
              variant="light"
              onPress={() => setSelected(new Set())}
            >
              取消选择
            </Button>
          </div>
        </div>
      )}

      {displayNodes.length === 0 ? (
        <Card className="shadow-sm border border-gray-200 dark:border-gray-700 bg-default-50/50">
          <CardBody className="text-center py-20 flex flex-col items-center justify-center min-h-[240px]">
            <h3 className="text-xl font-medium text-foreground tracking-tight mb-2">
              {hasFilter ? "没有符合筛选条件的节点" : "暂无节点"}
            </h3>
            <p className="text-default-500 text-sm max-w-md mx-auto leading-relaxed">
              {hasFilter
                ? "换个关键词或清除筛选。"
                : admin
                  ? "可以粘贴分享链接新增节点、从订阅导入机场节点，或在「部署节点」一键部署 Shadowsocks 2022 / mieru / Sudoku。"
                  : "可以粘贴分享链接新增节点，或从订阅导入机场节点。"}
            </p>
          </CardBody>
        </Card>
      ) : (
        <Card className="border border-divider shadow-sm">
          <DndContext
            collisionDetection={closestCenter}
            sensors={sortSensors}
            onDragEnd={handleDragEnd}
          >
            <SortableContext
              items={displayNodes.map((node) => node.id)}
              strategy={verticalListSortingStrategy}
            >
              <Table
                aria-label="节点列表"
                className="table-fixed"
                classNames={{
                  th: "border-b border-divider bg-default-100/50 px-1.5 py-2.5 text-xs font-semibold sm:px-2",
                  td: "px-1.5 py-2.5 sm:px-2",
                  tr: "border-divider/60 hover:bg-default-50/60 transition-colors",
                }}
              >
                <TableHeader>
                  <TableColumn className="w-12 px-1 sm:w-16">
                    <Checkbox
                      aria-label="选择所有筛选结果"
                      isSelected={allFilteredSelected}
                      onValueChange={toggleSelectAll}
                    />
                  </TableColumn>
                  <TableColumn>节点 / 中转链路</TableColumn>
                  <TableColumn className="hidden w-[24%] lg:table-cell">
                    协议与标签
                  </TableColumn>
                  <TableColumn className="hidden w-[22%] lg:table-cell">
                    连接地址
                  </TableColumn>
                  <TableColumn className="w-12 px-1 sm:w-14">启用</TableColumn>
                  <TableColumn className="w-28 px-1 text-right sm:w-36">
                    操作
                  </TableColumn>
                </TableHeader>
                <TableBody items={displayNodes}>
                  {(node: ProxyNode) => {
                    const nested = relayParents.has(node.id);
                    const isLastRelay = lastRelayIds.has(node.id);
                    const relayCount = relayCounts.get(node.id) ?? 0;
                    const address = hostPort(node.server, node.port);
                    const connection = (
                      <span
                        className="block truncate font-mono text-xs text-default-600"
                        title={address}
                      >
                        {address}
                      </span>
                    );
                    const protocolAndTags = (
                      <div className="flex flex-wrap gap-1">
                        <Chip
                          className="h-5 max-w-full border-divider font-mono text-[11px] text-default-700"
                          size="sm"
                          variant="bordered"
                        >
                          <span className="block truncate">
                            {protocolLabel(node.protocol)}
                          </span>
                        </Chip>
                        {node.tags.map((tag) => (
                          <Chip
                            key={tag}
                            className="h-5 max-w-full bg-default-100 text-[11px] text-default-600"
                            size="sm"
                            variant="flat"
                          >
                            <span className="block truncate" title={tag}>
                              {tag}
                            </span>
                          </Chip>
                        ))}
                      </div>
                    );

                    return (
                      <SortableTableRow
                        className={cn(
                          nested &&
                            "bg-default-50/85 hover:bg-default-100/70 dark:bg-default-100/25 dark:hover:bg-default-100/40",
                          isLastRelay && "border-b-divider",
                          ((nested && !isLastRelay) || relayCount > 0) &&
                            "border-b-divider/40",
                        )}
                        id={node.id}
                      >
                        {(handle) => (
                          <>
                            <TableCell
                              className={cn(
                                "px-1",
                                nested &&
                                  "relative before:pointer-events-none before:absolute before:top-0 before:left-0 before:w-[3px] before:bg-primary/65 dark:before:bg-primary/75",
                                nested &&
                                  (isLastRelay
                                    ? "before:bottom-0"
                                    : "before:-bottom-px"),
                              )}
                            >
                              <div className="flex items-center gap-0.5 sm:gap-1">
                                <Checkbox
                                  aria-label={`选择${node.name}`}
                                  isSelected={selected.has(node.id)}
                                  onValueChange={() => toggleSelect(node.id)}
                                />
                                <DragHandle
                                  className="shrink-0 p-0.5 sm:p-1"
                                  handle={handle}
                                />
                              </div>
                            </TableCell>
                            <TableCell>
                              <div
                                className={
                                  nested ? "relative pl-5 sm:pl-6" : undefined
                                }
                              >
                                {nested && (
                                  <CornerDownRight
                                    aria-hidden="true"
                                    className="absolute left-0 top-0.5 text-primary-500 dark:text-primary-400"
                                    size={15}
                                    strokeWidth={2.25}
                                  />
                                )}
                                <div className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5">
                                  <span
                                    className="truncate text-sm font-medium text-foreground"
                                    title={node.name}
                                  >
                                    {node.name}
                                  </span>
                                  <Chip
                                    className={cn(
                                      "h-5 text-[10px]",
                                      node.source === "relay"
                                        ? "bg-primary-50 text-primary-700 dark:bg-primary-500/15 dark:text-primary-300"
                                        : "bg-default-100 text-default-600",
                                    )}
                                    size="sm"
                                    variant="flat"
                                  >
                                    {sourceLabel(node.source)}
                                  </Chip>
                                  {!nested && relayCount > 0 && (
                                    <span className="whitespace-nowrap text-[11px] font-medium text-primary-600 dark:text-primary-400">
                                      {relayCount} 条中转
                                    </span>
                                  )}
                                </div>
                                {node.source === "relay" && (
                                  <div className="mt-0.5 break-words text-xs text-default-500">
                                    {nested ? (
                                      <>
                                        经由隧道：
                                        <span className="font-medium text-default-700">
                                          {node.tunnelName || "-"}
                                        </span>
                                      </>
                                    ) : (
                                      `源节点：${node.parentName || "-"} · 隧道：${node.tunnelName || "-"}`
                                    )}
                                    {node.relayStatus === "forward_missing" && (
                                      <span className="ml-1 text-danger">
                                        （转发已删除）
                                      </span>
                                    )}
                                  </div>
                                )}
                                {node.remark && (
                                  <div
                                    className="mt-0.5 truncate text-xs text-default-500"
                                    title={node.remark}
                                  >
                                    {node.remark}
                                  </div>
                                )}
                                <div className="mt-1 space-y-1 lg:hidden">
                                  {connection}
                                  {protocolAndTags}
                                </div>
                              </div>
                            </TableCell>
                            <TableCell className="hidden lg:table-cell">
                              {protocolAndTags}
                            </TableCell>
                            <TableCell className="hidden lg:table-cell">
                              {connection}
                            </TableCell>
                            <TableCell className="px-1">
                              <Switch
                                aria-label={`启用${node.name}`}
                                isSelected={node.enabled}
                                size="sm"
                                onValueChange={(value) =>
                                  toggleOne(node, value)
                                }
                              />
                            </TableCell>
                            <TableCell className="px-1">
                              <div className="flex flex-wrap items-center justify-end gap-1">
                                {node.source !== "relay" && (
                                  <Button
                                    className="h-7 px-1 text-xs sm:px-2"
                                    size="sm"
                                    variant="flat"
                                    onPress={() => openRelay(node)}
                                  >
                                    中转
                                  </Button>
                                )}
                                <Button
                                  className="h-7 px-1 text-xs sm:px-2"
                                  isDisabled={!node.shareLink}
                                  size="sm"
                                  title="复制分享链接"
                                  variant="flat"
                                  onPress={() =>
                                    copyText(node.shareLink, "分享链接")
                                  }
                                >
                                  链接
                                </Button>
                                <Dropdown placement="bottom-end">
                                  <DropdownTrigger>
                                    <Button
                                      isIconOnly
                                      aria-label={`${node.name}的更多操作`}
                                      className="size-7"
                                      variant="flat"
                                    >
                                      <Ellipsis size={16} />
                                    </Button>
                                  </DropdownTrigger>
                                  <DropdownMenu>
                                    <DropdownMenuItem
                                      onSelect={() => openEdit(node)}
                                    >
                                      编辑
                                    </DropdownMenuItem>
                                    <DropdownMenuItem
                                      className="text-danger focus:text-danger"
                                      onSelect={() => openDelete([node.id])}
                                    >
                                      删除
                                    </DropdownMenuItem>
                                  </DropdownMenu>
                                </Dropdown>
                              </div>
                            </TableCell>
                          </>
                        )}
                      </SortableTableRow>
                    );
                  }}
                </TableBody>
              </Table>
            </SortableContext>
          </DndContext>
        </Card>
      )}

      {/* 新增 / 编辑 */}
      <Modal
        backdrop="blur"
        classNames={modalClassNames}
        isOpen={editOpen}
        placement="center"
        scrollBehavior="inside"
        size="2xl"
        onOpenChange={setEditOpen}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader className="flex flex-col gap-1">
                <h2 className="text-xl font-bold">
                  {editForm.id ? "编辑节点" : "新增节点"}
                </h2>
                {editForm.isRelay && (
                  <p className="text-small text-default-500">
                    中转节点的配置跟随源节点与转发入口自动更新，这里只能修改名称、标签、备注和连接地址。
                  </p>
                )}
              </ModalHeader>
              <ModalBody>
                <div className="space-y-4">
                  <Input
                    label="节点名称"
                    placeholder={editForm.id ? "" : "留空则使用链接中的名称"}
                    value={editForm.name}
                    variant="bordered"
                    onChange={(e) =>
                      setEditForm((p) => ({ ...p, name: e.target.value }))
                    }
                  />
                  {editForm.isRelay ? (
                    <Input
                      description="留空则使用转发入口地址；入口是内网地址或想用域名时填写"
                      label="连接地址（可选）"
                      placeholder="例如 hk.example.com"
                      value={editForm.relayHost}
                      variant="bordered"
                      onChange={(e) =>
                        setEditForm((p) => ({
                          ...p,
                          relayHost: e.target.value,
                        }))
                      }
                    />
                  ) : (
                    <>
                      <Tabs
                        selectedKey={editMode}
                        onSelectionChange={(k) =>
                          setEditMode(String(k) as "link" | "config")
                        }
                      >
                        <Tab key="link" title="分享链接">
                          <Textarea
                            description="支持 ss / vmess / vless / trojan / hysteria2 / tuic / anytls / mierus / sudoku 等链接"
                            label={
                              editForm.id
                                ? "新的分享链接（留空不修改）"
                                : "分享链接"
                            }
                            minRows={3}
                            placeholder="ss://... / vless://... / sudoku://..."
                            value={editForm.link}
                            variant="bordered"
                            onChange={(e) =>
                              setEditForm((p) => ({
                                ...p,
                                link: e.target.value,
                              }))
                            }
                          />
                        </Tab>
                        <Tab key="config" title="节点配置 (JSON)">
                          <Textarea
                            className="font-mono"
                            description="Clash / mihomo 代理字段，例如 name、type、server、port、cipher、password"
                            label="节点配置"
                            minRows={10}
                            value={editForm.configText}
                            variant="bordered"
                            onChange={(e) =>
                              setEditForm((p) => ({
                                ...p,
                                configText: e.target.value,
                              }))
                            }
                          />
                        </Tab>
                      </Tabs>
                    </>
                  )}
                  <Input
                    description="多个标签用逗号或空格分隔，生成订阅时可按标签筛选"
                    label="标签"
                    value={editForm.tags}
                    variant="bordered"
                    onChange={(e) =>
                      setEditForm((p) => ({ ...p, tags: e.target.value }))
                    }
                  />
                  <Input
                    label="备注"
                    value={editForm.remark}
                    variant="bordered"
                    onChange={(e) =>
                      setEditForm((p) => ({ ...p, remark: e.target.value }))
                    }
                  />
                  <LabeledSwitch
                    description="停用的节点不会出现在订阅中"
                    isSelected={editForm.enabled}
                    label="启用"
                    onValueChange={(v) =>
                      setEditForm((p) => ({ ...p, enabled: v }))
                    }
                  />
                </div>
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  取消
                </Button>
                <Button color="primary" isLoading={saving} onPress={submitEdit}>
                  保存
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>

      {/* 导入 */}
      <Modal
        backdrop="blur"
        classNames={modalClassNames}
        isOpen={importOpen}
        placement="center"
        scrollBehavior="inside"
        size="2xl"
        onOpenChange={setImportOpen}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader className="flex flex-col gap-1">
                <h2 className="text-xl font-bold">导入节点</h2>
                <p className="text-small text-default-500">
                  导入个人节点或机场节点：粘贴分享链接 / base64 订阅 / Clash
                  YAML，或填写订阅地址由面板拉取。
                </p>
              </ModalHeader>
              <ModalBody>
                <div className="space-y-4">
                  <Textarea
                    label="节点内容"
                    minRows={6}
                    placeholder="每行一个分享链接，或粘贴整份订阅内容"
                    value={importContent}
                    variant="bordered"
                    onChange={(e) => setImportContent(e.target.value)}
                  />
                  <Input
                    description="填写后优先从该地址拉取（面板会拒绝内网地址）"
                    label="订阅地址（可选）"
                    placeholder="https://..."
                    value={importUrl}
                    variant="bordered"
                    onChange={(e) => setImportUrl(e.target.value)}
                  />
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <Input
                      label="User-Agent（可选）"
                      placeholder="默认 clash.meta"
                      value={importUA}
                      variant="bordered"
                      onChange={(e) => setImportUA(e.target.value)}
                    />
                    <Input
                      label="给导入的节点加标签"
                      placeholder="例如 机场A"
                      value={importTags}
                      variant="bordered"
                      onChange={(e) => setImportTags(e.target.value)}
                    />
                  </div>
                  <Checkbox
                    isSelected={importSkipDup}
                    onValueChange={setImportSkipDup}
                  >
                    跳过已存在的节点（类型、地址、端口、名称都相同）
                  </Checkbox>
                  {importPreview && (
                    <div className="rounded-lg border border-divider p-3 max-h-64 overflow-auto text-sm">
                      <div className="font-medium mb-2">
                        识别到 {importPreview.proxies.length} 个节点
                        {importPreview.failed.length > 0 &&
                          `，${importPreview.failed.length} 行无法识别`}
                      </div>
                      <ul className="space-y-1">
                        {importPreview.proxies.slice(0, 200).map((p, idx) => (
                          <li
                            key={`${p.name}-${idx}`}
                            className="flex justify-between gap-2 text-xs"
                          >
                            <span className="truncate">{p.name}</span>
                            <span className="text-default-500 font-mono whitespace-nowrap">
                              {protocolLabel(p.protocol)}{" "}
                              {hostPort(p.server, p.port)}
                            </span>
                          </li>
                        ))}
                      </ul>
                    </div>
                  )}
                </div>
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  取消
                </Button>
                <Button
                  isLoading={importing}
                  variant="flat"
                  onPress={previewImport}
                >
                  预览
                </Button>
                <Button
                  color="primary"
                  isLoading={importing}
                  onPress={submitImport}
                >
                  导入
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>

      {/* 批量设置标签 */}
      <Modal
        backdrop="blur"
        classNames={modalClassNames}
        isOpen={tagsOpen}
        placement="center"
        size="md"
        onOpenChange={setTagsOpen}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader>
                设置标签（覆盖 {selected.size} 个节点的标签）
              </ModalHeader>
              <ModalBody>
                <Input
                  description="多个标签用逗号或空格分隔，留空清除标签"
                  label="标签"
                  value={batchTags}
                  variant="bordered"
                  onChange={(e) => setBatchTags(e.target.value)}
                />
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  取消
                </Button>
                <Button color="primary" onPress={submitBatchTags}>
                  保存
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>

      {/* 删除确认 */}
      <Modal
        backdrop="blur"
        classNames={modalClassNames}
        isOpen={deleteOpen}
        placement="center"
        size="md"
        onOpenChange={setDeleteOpen}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader>
                <h2 className="text-lg font-bold text-danger">确认删除</h2>
              </ModalHeader>
              <ModalBody>
                <p className="text-default-600">
                  确定要删除 {deleteIds.length}{" "}
                  个节点吗？源节点的中转节点会一并删除。
                </p>
                {deleteHasRelay && (
                  <Checkbox
                    isSelected={deleteForwards}
                    onValueChange={setDeleteForwards}
                  >
                    同时删除中转节点对应的转发规则
                  </Checkbox>
                )}
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  取消
                </Button>
                <Button
                  color="danger"
                  isLoading={deleting}
                  onPress={confirmDelete}
                >
                  确认删除
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>

      {/* 隧道中转 */}
      <Modal
        backdrop="blur"
        classNames={modalClassNames}
        isOpen={relayOpen}
        placement="center"
        scrollBehavior="inside"
        size="2xl"
        onOpenChange={setRelayOpen}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader className="flex flex-col gap-1">
                <h2 className="text-xl font-bold">隧道中转</h2>
                <p className="text-small text-default-500">
                  源节点：{relayNode?.name}（
                  {relayNode ? hostPort(relayNode.server, relayNode.port) : ""}
                  ）。选择隧道后自动创建转发，并按转发入口生成新的中转节点。
                </p>
              </ModalHeader>
              <ModalBody>
                <Tabs
                  selectedKey={relayTab}
                  onSelectionChange={(k) => setRelayTab(String(k))}
                >
                  <Tab key="create" title="选择隧道（新建转发）">
                    <div className="space-y-4">
                      <Select
                        isRequired
                        label="隧道"
                        placeholder={
                          tunnels.length ? "请选择隧道" : "暂无可用隧道"
                        }
                        selectedKeys={relayTunnelId ? [relayTunnelId] : []}
                        onSelectionChange={(keys) =>
                          setRelayTunnelId(String(Array.from(keys)[0] ?? ""))
                        }
                      >
                        {tunnels.map((t) => (
                          <SelectItem key={String(t.id)}>{t.name}</SelectItem>
                        ))}
                      </Select>
                      <Input
                        description="留空自动分配"
                        label="入口端口（可选）"
                        type="number"
                        value={relayPort}
                        variant="bordered"
                        onChange={(e) => setRelayPort(e.target.value)}
                      />
                    </div>
                  </Tab>
                  <Tab
                    key="bind"
                    title={`关联已有转发（${relayCandidates.length}）`}
                  >
                    {relayCandidates.length === 0 ? (
                      <p className="text-sm text-default-500 py-4">
                        没有目标地址指向该节点的转发规则。
                      </p>
                    ) : (
                      <div className="space-y-2">
                        {relayCandidates.map((c) => (
                          <div
                            key={c.id}
                            className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 rounded-lg border border-divider p-3"
                          >
                            <div className="text-sm">
                              <div className="font-medium">{c.name}</div>
                              <div className="text-xs text-default-500">
                                隧道：{c.tunnelName || "-"} · 用户：{c.userName}{" "}
                                · 入口：
                                {c.entries
                                  .map((e) => hostPort(e.host, e.port))
                                  .join("、") || "-"}
                              </div>
                            </div>
                            <Button
                              color="primary"
                              isLoading={relaySubmitting}
                              size="sm"
                              variant="flat"
                              onPress={() => bindCandidate(c)}
                            >
                              生成中转节点
                            </Button>
                          </div>
                        ))}
                      </div>
                    )}
                  </Tab>
                </Tabs>
                <div className="space-y-4 mt-2">
                  <Input
                    description="留空则使用转发入口的 IP（入口节点地址或监听 IP）"
                    label="连接地址（可选）"
                    placeholder="例如 hk-relay.example.com"
                    value={relayHost}
                    variant="bordered"
                    onChange={(e) => setRelayHost(e.target.value)}
                  />
                  <Input
                    description="默认「源节点名 | 隧道名」"
                    label="中转节点名称（可选）"
                    value={relayName}
                    variant="bordered"
                    onChange={(e) => setRelayName(e.target.value)}
                  />
                  <Checkbox
                    isSelected={relayAllEntries}
                    onValueChange={setRelayAllEntries}
                  >
                    多入口隧道为每个入口各生成一个节点
                  </Checkbox>
                </div>
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  取消
                </Button>
                {relayTab === "create" && (
                  <Button
                    color="primary"
                    isLoading={relaySubmitting}
                    onPress={submitRelay}
                  >
                    创建转发并生成节点
                  </Button>
                )}
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>
    </AnimatedPage>
  );
}
