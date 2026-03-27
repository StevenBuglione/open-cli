import type { DataProvider } from "@refinedev/core";

const API_URL = "http://localhost:8080/api/v1";

export const dataProvider: DataProvider = {
  getList: async ({ resource, pagination, filters, sorters }) => {
    // TODO: Implement actual API calls to control plane
    // For now, return mock data for development
    console.log("getList", { resource, pagination, filters, sorters });
    
    return {
      data: [],
      total: 0,
    };
  },

  getOne: async ({ resource, id }) => {
    console.log("getOne", { resource, id });
    
    return {
      data: { id } as any,
    };
  },

  create: async ({ resource, variables }) => {
    console.log("create", { resource, variables });
    
    return {
      data: { id: 1, ...variables } as any,
    };
  },

  update: async ({ resource, id, variables }) => {
    console.log("update", { resource, id, variables });
    
    return {
      data: { id, ...variables } as any,
    };
  },

  deleteOne: async ({ resource, id }) => {
    console.log("deleteOne", { resource, id });
    
    return {
      data: { id } as any,
    };
  },

  getApiUrl: () => {
    return API_URL;
  },
};
