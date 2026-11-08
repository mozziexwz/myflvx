// myflvx 增强功能接口：节点管理、订阅管理 / 生成 / 下载、隧道中转、部署节点
import Network from "./network";

export interface ProxyNode {
  id: number;
  name: string;
  protocol: string;
  server: string;
  port: number;
  config: Record<string, unknown>;
  rawUrl: string;
  shareLink: string;
  tags: string[];
  remark: string;
  source: "manual" | "import" | "deploy" | "relay" | string;
  sourceRef: string;
  parentId: number;
  parentName?: string;
  tunnelId: number;
  tunnelName?: string;
  forwardId: number;
  forwardName?: string;
  relayHost: string;
  relayEntry: number;
  relayStatus?: "ok" | "forward_missing" | string;
  enabled: boolean;
  inx: number;
  createdTime: number;
  updatedTime: number;
}

export interface ProxyNodeListData {
  nodes: ProxyNode[];
  tags: string[];
}

export interface ProxyNodeMutation {
  id?: number;
  name?: string;
  config?: Record<string, unknown>;
  link?: string;
  tags?: string[];
  remark?: string;
  enabled?: boolean;
  relayHost?: string;
}

export interface ParsedProxyBrief {
  name: string;
  protocol: string;
  server: string;
  port: number;
}

export interface ImportPayload {
  content?: string;
  url?: string;
  userAgent?: string;
  tags?: string[];
  remark?: string;
  skipDuplicates?: boolean;
}

export interface ForwardEntry {
  nodeId: number;
  nodeName: string;
  host: string;
  port: number;
}

export interface RelayCandidate {
  id: number;
  name: string;
  tunnelId: number;
  tunnelName: string;
  userName: string;
  remoteAddr: string;
  entries: ForwardEntry[];
  status: number;
}

export const getProxyNodes = () =>
  Network.post<ProxyNodeListData>("/proxy-node/list");
export const createProxyNode = (data: ProxyNodeMutation) =>
  Network.post<ProxyNode>("/proxy-node/create", data);
export const updateProxyNode = (data: ProxyNodeMutation) =>
  Network.post<{ node: ProxyNode; warnings: string[] }>(
    "/proxy-node/update",
    data,
    { timeout: 120_000 },
  );
export const deleteProxyNodes = (ids: number[], deleteForwards: boolean) =>
  Network.post<{ deleted: number; warnings: string[] }>(
    "/proxy-node/delete",
    { ids, deleteForwards },
    { timeout: 120_000 },
  );
export const toggleProxyNodes = (ids: number[], enabled: boolean) =>
  Network.post("/proxy-node/toggle", { ids, enabled });
export const setProxyNodeTags = (ids: number[], tags: string[]) =>
  Network.post("/proxy-node/tags", { ids, tags });
export const updateProxyNodeOrder = (items: { id: number; inx: number }[]) =>
  Network.post("/proxy-node/update-order", { items });
export const parseProxyContent = (data: ImportPayload) =>
  Network.post<{
    proxies: ParsedProxyBrief[];
    failed: string[];
    format: string;
  }>("/proxy-node/parse", data, { timeout: 90_000 });
export const importProxyNodes = (data: ImportPayload) =>
  Network.post<{ created: number; skipped: number; failed: string[] }>(
    "/proxy-node/import",
    data,
    { timeout: 90_000 },
  );
export const getProxyShareLinks = (ids: number[]) =>
  Network.post<{ links: string }>("/proxy-node/share-links", { ids });
export const createRelay = (data: {
  nodeId: number;
  tunnelId: number;
  inPort?: number;
  relayHost?: string;
  name?: string;
  allEntries?: boolean;
}) =>
  Network.post<{ forwardId: number; nodes: ProxyNode[] }>(
    "/proxy-node/relay/create",
    data,
    { timeout: 120_000 },
  );
export const getRelayCandidates = (nodeId: number) =>
  Network.post<RelayCandidate[]>("/proxy-node/relay/candidates", { nodeId });
export const bindRelay = (data: {
  nodeId: number;
  forwardId: number;
  relayHost?: string;
  name?: string;
  allEntries?: boolean;
}) =>
  Network.post<{ forwardId: number; nodes: ProxyNode[] }>(
    "/proxy-node/relay/bind",
    data,
  );

// ─── 订阅 ─────────────────────────────────────────────────────────────

export interface ProxyGroupSpec {
  name: string;
  type: "select" | "url-test" | "fallback" | "load-balance" | string;
  proxies: string[];
  includeAllNodes: boolean;
  filter?: string;
  url?: string;
  interval?: number;
  tolerance?: number;
  strategy?: string;
  lazy?: boolean;
  hidden?: boolean;
  icon?: string;
}

export interface GeneratorConfig {
  nodeIds: number[];
  allNodes: boolean;
  tags?: string[];
  template: string;
  groups: ProxyGroupSpec[];
  rules: string[];
  baseConfig?: string;
  ruleProviders?: string;
}

export interface SubscriptionTemplate {
  id: string;
  name: string;
  description: string;
  groups: ProxyGroupSpec[];
  rules: string[];
  baseConfig: string;
}

export interface SubscriptionInfo {
  upload: number;
  download: number;
  total: number;
  expire: number;
}

export interface Subscription {
  id: number;
  name: string;
  remark: string;
  type: "upload" | "import" | "generate" | string;
  sourceUrl?: string;
  userAgent?: string;
  fileName: string;
  subInfo?: SubscriptionInfo;
  autoUpdate: number;
  enabled: boolean;
  lastSyncTime: number;
  inx: number;
  createdTime: number;
  updatedTime: number;
  nodeCount: number;
  genConfig?: GeneratorConfig;
  content?: string;
}

export interface SubscriptionMutation {
  id?: number;
  name: string;
  remark?: string;
  type: string;
  content?: string;
  sourceUrl?: string;
  userAgent?: string;
  genConfig?: GeneratorConfig;
  fileName?: string;
  enabled?: boolean;
  autoUpdate?: number;
  refetch?: boolean;
}

export const getSubscriptions = () =>
  Network.post<Subscription[]>("/subscription/list");
export const getSubscription = (id: number) =>
  Network.post<Subscription>("/subscription/get", { id });
export const createSubscription = (data: SubscriptionMutation) =>
  Network.post<Subscription>("/subscription/create", data, {
    timeout: 90_000,
  });
export const updateSubscription = (data: SubscriptionMutation) =>
  Network.post<Subscription>("/subscription/update", data, {
    timeout: 90_000,
  });
export const deleteSubscriptions = (ids: number[]) =>
  Network.post("/subscription/delete", { ids });
export const refreshSubscription = (id: number) =>
  Network.post<Subscription>(
    "/subscription/refresh",
    { id },
    {
      timeout: 90_000,
    },
  );
export const previewSubscription = (genConfig: GeneratorConfig) =>
  Network.post<{ content: string; warnings: string[]; nodeCount: number }>(
    "/subscription/preview",
    { genConfig },
  );
export const getSubscriptionTemplates = () =>
  Network.post<{
    templates: SubscriptionTemplate[];
    defaultBaseConfig: string;
  }>("/subscription/templates");
export const updateSubscriptionOrder = (items: { id: number; inx: number }[]) =>
  Network.post("/subscription/update-order", { items });

export const getMySubscriptions = () =>
  Network.post<Subscription[]>("/subscription/my/list");
export const downloadMySubscription = (
  id: number,
  format: "clash" | "base64" | "uri",
) =>
  Network.post<{ fileName: string; content: string; mime: string }>(
    "/subscription/my/download",
    { id, format },
    { timeout: 90_000 },
  );

// ─── 部署节点 ─────────────────────────────────────────────────────────

export interface DeployParams {
  protocols: string[];
  host: string;
  scriptName?: string;
  ipVersion: string;
  blockPrivate: boolean;
  mihomoVersion?: string;
  ssPort?: number;
  ssCipher?: string;
  ssPassword?: string;
  mieruPort?: number;
  mieruUser?: string;
  mieruPassword?: string;
  mieruLowEntropy?: string;
  sudokuPort?: number;
  sudokuKeyMode?: "psk" | "pair" | string;
  sudokuKey?: string;
  sudokuServerKey?: string;
  sudokuTable?: string;
  sudokuPadding?: string;
  sudokuPureDownlink: boolean;
  sudokuCustomTables?: string;
}

export type DeployStatus =
  | "pending"
  | "running"
  | "success"
  | "failed"
  | "uninstalled";

export interface Deployment {
  id: number;
  name: string;
  nodeId: number;
  nodeName?: string;
  nodeOnline: boolean;
  nodeVersion?: string;
  host: string;
  protocols: string[];
  params: DeployParams;
  status: DeployStatus | string;
  taskOp?: "install" | "info" | "uninstall" | string;
  lastLog?: string;
  lastRunTime: number;
  remark: string;
  inx: number;
  createdTime: number;
  updatedTime: number;
  proxyNodes: number;
}

export interface DeploySettings {
  scriptUrl: string;
  defaultScriptUrl: string;
  useProxy: boolean;
  effectiveUrl: string;
}

interface DeployBatchResult {
  items: Deployment[];
  warnings: string[];
}

interface DeployStartResult {
  started: number;
  warnings: string[];
}

const deployTimeout = { timeout: 120_000 };

export const getDeployments = () => Network.post<Deployment[]>("/deploy/list");
export const getDeployment = (id: number) =>
  Network.post<Deployment>("/deploy/get", { id });
export const getDeployDefaults = () =>
  Network.post<{ params: DeployParams; scriptUrl: string }>("/deploy/defaults");
export const createDeployments = (data: {
  nodeIds: number[];
  host?: string;
  params: DeployParams;
  remark?: string;
  run: boolean;
}) => Network.post<DeployBatchResult>("/deploy/create", data, deployTimeout);
export const copyDeployment = (id: number, nodeIds: number[], run: boolean) =>
  Network.post<DeployBatchResult>(
    "/deploy/copy",
    { id, nodeIds, run },
    deployTimeout,
  );
export const updateDeployment = (data: {
  id: number;
  host: string;
  params: DeployParams;
  remark?: string;
}) => Network.post<Deployment>("/deploy/update", data);
export const deleteDeployment = (id: number, deleteNodes: boolean) =>
  Network.post("/deploy/delete", { id, deleteNodes });
export const runDeployments = (ids: number[]) =>
  Network.post<DeployStartResult>("/deploy/run", { ids }, deployTimeout);
export const refreshDeployment = (id: number) =>
  Network.post<DeployStartResult>("/deploy/refresh", { id }, deployTimeout);
export const uninstallDeployment = (id: number) =>
  Network.post<DeployStartResult>("/deploy/uninstall", { id }, deployTimeout);
export const updateDeployOrder = (items: { id: number; inx: number }[]) =>
  Network.post("/deploy/update-order", { items });
export const getDeployCommand = (id: number) =>
  Network.post<{ command: string; scriptUrl: string }>("/deploy/command", {
    id,
  });
export const syncDeployNodes = (id: number) =>
  Network.post<{ nodes: number; warnings: string[] }>("/deploy/sync-nodes", {
    id,
  });
export const deploySettings = (data: {
  scriptUrl?: string;
  useProxy?: boolean;
}) => Network.post<DeploySettings>("/deploy/settings", data);
