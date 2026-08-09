import { useQuery } from "@tanstack/react-query";
import type { components } from "../../api/schema";
import { apiClient, hasTrustedApiBaseUrl } from "../lib/api-client";
import { usesPreviewWorkspaceData } from "../lib/preview-mode";

export type DCPTask = components["schemas"]["DCPTask"];

export const dcpTasksQueryKey = (projectId = "") => ["dcp-tasks", projectId] as const;

async function fetchDCPTasks(projectId?: string): Promise<DCPTask[]> {
	if (usesPreviewWorkspaceData || (projectId !== undefined && projectId !== "dcp-lab")) return [];
	if (!hasTrustedApiBaseUrl()) throw new Error("DCP daemon API is not ready");
	const { data, error } = await apiClient.GET("/api/v1/dcp/tasks", {
		params: { query: projectId ? { project: projectId } : {} },
	});
	if (error) throw error;
	return data?.tasks ?? [];
}

export function useDCPTasksQuery(projectId?: string) {
	return useQuery({
		queryKey: dcpTasksQueryKey(projectId),
		queryFn: () => fetchDCPTasks(projectId),
		retry: false,
	});
}
