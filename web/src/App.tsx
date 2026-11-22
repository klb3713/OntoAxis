import { lazy, Suspense } from "react";
import { Authenticated, Refine, type ResourceProps } from "@refinedev/core";
import routerProvider, { CatchAllNavigate } from "@refinedev/react-router";
import { BrowserRouter, Navigate, Outlet, Route, Routes } from "react-router-dom";
import type { AppRuntime } from "./app/runtime";
import { AuthRuntimeContext } from "./auth/AuthContext";
import { AppLayout } from "./components/AppLayout";
import { LoadingBlock } from "./components/AsyncState";
import { AuthCallbackPage } from "./features/auth/AuthCallbackPage";
import { LoginPage } from "./features/auth/LoginPage";
import { NotFoundPage } from "./features/not-found/NotFoundPage";

const ProjectsPage = lazy(() => import("./features/projects/ProjectsPage").then((module) => ({ default: module.ProjectsPage })));
const OntologyPage = lazy(() => import("./features/ontology/OntologyPage").then((module) => ({ default: module.OntologyPage })));
const ObjectExplorerPage = lazy(() =>
  import("./features/object-explorer/ObjectExplorerPage").then((module) => ({ default: module.ObjectExplorerPage })),
);
const DatasetsPage = lazy(() => import("./features/datasets/DatasetsPage").then((module) => ({ default: module.DatasetsPage })));
const QueriesPage = lazy(() => import("./features/queries/QueriesPage").then((module) => ({ default: module.QueriesPage })));

const RESOURCES: ResourceProps[] = [
  { name: "projects", list: "/projects" },
  { name: "object-types", list: "/ontology" },
  { name: "link-types", list: "/ontology" },
  { name: "objects", list: "/object-explorer" },
  { name: "datasets", list: "/datasets" },
  { name: "saved-queries", list: "/queries" },
];

function RouteFallback() {
  return (
    <div className="route-fallback">
      <LoadingBlock />
    </div>
  );
}

export type AppProps = {
  runtime: AppRuntime;
};

export function App({ runtime }: AppProps) {
  return (
    <AuthRuntimeContext.Provider value={{ config: runtime.config, authAdapter: runtime.authAdapter }}>
      <BrowserRouter>
        <Refine
          routerProvider={routerProvider}
          dataProvider={runtime.dataProvider}
          authProvider={runtime.authProvider}
          resources={RESOURCES}
          options={{ syncWithLocation: true, warnWhenUnsavedChanges: true }}
        >
          <Suspense fallback={<RouteFallback />}>
            <Routes>
              <Route path="/login" element={<LoginPage />} />
              <Route path="/auth/callback" element={<AuthCallbackPage />} />
              <Route
                element={
                  <Authenticated key="protected-routes" fallback={<CatchAllNavigate to="/login" />} loading={<RouteFallback />}>
                    <Outlet />
                  </Authenticated>
                }
              >
                <Route path="/" element={<AppLayout />}>
                  <Route index element={<Navigate to="/projects" replace />} />
                  <Route path="projects" element={<ProjectsPage />} />
                  <Route path="ontology" element={<OntologyPage />} />
                  <Route path="object-explorer" element={<ObjectExplorerPage />} />
                  <Route path="datasets" element={<DatasetsPage />} />
                  <Route path="queries" element={<QueriesPage />} />
                  <Route path="*" element={<NotFoundPage />} />
                </Route>
              </Route>
            </Routes>
          </Suspense>
        </Refine>
      </BrowserRouter>
    </AuthRuntimeContext.Provider>
  );
}
