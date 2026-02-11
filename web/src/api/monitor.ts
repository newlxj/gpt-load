import http from "@/utils/http";
import type { GroupMonitorResponse, GroupUsageData } from "@/types/models";

export const monitorApi = {
  // 获取所有分组的监控数据
  async getGroupMonitorData(): Promise<GroupMonitorResponse> {
    const res = await http.get("/groups/monitor");
    return res.data;
  },

  // 获取单个分组的使用量数据
  async getGroupUsage(groupId: number): Promise<GroupUsageData> {
    const res = await http.get(`/groups/${groupId}/usage`);
    return res.data;
  },

  // 获取分组排序
  async getGroupSortOrder(): Promise<number[]> {
    const res = await http.get("/groups/monitor/sort-order");
    return res.data || [];
  },

  // 保存分组排序
  async saveGroupSortOrder(order: number[]): Promise<void> {
    await http.put("/groups/monitor/sort-order", order);
  },
};
