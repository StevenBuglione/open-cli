import { Refine } from "@refinedev/core";
import { BrowserRouter, Routes, Route, Outlet } from "react-router-dom";
import routerBindings from "@refinedev/react-router-v6";

import { authProvider } from "./auth/authProvider";
import { dataProvider } from "./dataProvider";

import { SourcesList } from "./pages/sources/List";
import { BundlesList } from "./pages/bundles/List";
import { PrincipalsList } from "./pages/principals/List";
import { PublishPage } from "./pages/publish/Page";
import { AuditList } from "./pages/audit/List";

const Layout = () => {
  return (
    <div style={{ padding: "16px" }}>
      <header style={{ marginBottom: "24px", borderBottom: "1px solid #ddd", paddingBottom: "16px" }}>
        <h1 style={{ margin: 0 }}>Open CLI Admin</h1>
        <nav style={{ marginTop: "8px" }}>
          <a href="/sources" style={{ marginRight: "16px" }}>Sources</a>
          <a href="/bundles" style={{ marginRight: "16px" }}>Bundles</a>
          <a href="/principals" style={{ marginRight: "16px" }}>Principals</a>
          <a href="/publish" style={{ marginRight: "16px" }}>Publish</a>
          <a href="/audit" style={{ marginRight: "16px" }}>Audit</a>
        </nav>
      </header>
      <main>
        <Outlet />
      </main>
    </div>
  );
};

function App() {
  return (
    <BrowserRouter>
      <Refine
        dataProvider={dataProvider}
        authProvider={authProvider}
        routerProvider={routerBindings}
        resources={[
          {
            name: "sources",
            list: "/sources",
          },
          {
            name: "bundles",
            list: "/bundles",
          },
          {
            name: "principals",
            list: "/principals",
          },
          {
            name: "audit",
            list: "/audit",
          },
        ]}
      >
        <Routes>
          <Route element={<Layout />}>
            <Route index element={<SourcesList />} />
            <Route path="/sources" element={<SourcesList />} />
            <Route path="/bundles" element={<BundlesList />} />
            <Route path="/principals" element={<PrincipalsList />} />
            <Route path="/publish" element={<PublishPage />} />
            <Route path="/audit" element={<AuditList />} />
          </Route>
        </Routes>
      </Refine>
    </BrowserRouter>
  );
}

export default App;
