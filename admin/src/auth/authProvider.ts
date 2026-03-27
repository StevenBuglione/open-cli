import type { AuthProvider } from "@refinedev/core";

export const authProvider: AuthProvider = {
  login: async ({ username, password }) => {
    // TODO: Implement actual authentication with control plane API
    // For now, accept any credentials for development
    if (username && password) {
      localStorage.setItem("auth", JSON.stringify({ username }));
      return {
        success: true,
        redirectTo: "/",
      };
    }
    return {
      success: false,
      error: {
        name: "LoginError",
        message: "Invalid username or password",
      },
    };
  },
  logout: async () => {
    localStorage.removeItem("auth");
    return {
      success: true,
      redirectTo: "/login",
    };
  },
  check: async () => {
    const auth = localStorage.getItem("auth");
    if (auth) {
      return {
        authenticated: true,
      };
    }
    return {
      authenticated: false,
      redirectTo: "/login",
    };
  },
  getPermissions: async () => {
    const auth = localStorage.getItem("auth");
    if (auth) {
      return ["admin"];
    }
    return null;
  },
  getIdentity: async () => {
    const auth = localStorage.getItem("auth");
    if (auth) {
      const user = JSON.parse(auth);
      return {
        id: 1,
        name: user.username,
        avatar: "",
      };
    }
    return null;
  },
  onError: async (error) => {
    console.error(error);
    return { error };
  },
};
