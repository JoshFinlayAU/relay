import { Navigate, Route, Routes, useLocation } from "react-router-dom";
import { isAuthed } from "./lib/auth";
import Login from "./pages/Login";
import Dashboard from "./pages/Dashboard";
import Domains from "./pages/Domains";
import DomainDetail from "./pages/DomainDetail";
import Messages from "./pages/Messages";
import MessageDetail from "./pages/MessageDetail";
import Users from "./pages/Users";
import Events from "./pages/Events";
import Settings from "./pages/Settings";
import Layout from "./components/Layout";
import type { ReactElement } from "react";

function RequireAuth({ children }: { children: ReactElement }) {
  const location = useLocation();
  if (!isAuthed()) {
    return <Navigate to="/login" state={{ from: location }} replace />;
  }
  return children;
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route
        element={
          <RequireAuth>
            <Layout />
          </RequireAuth>
        }
      >
        <Route path="/" element={<Dashboard />} />
        <Route path="/domains" element={<Domains />} />
        <Route path="/domains/:id" element={<DomainDetail />} />
        <Route path="/messages" element={<Messages />} />
        <Route path="/messages/:id" element={<MessageDetail />} />
        <Route path="/users" element={<Users />} />
        <Route path="/events" element={<Events />} />
        <Route path="/settings" element={<Settings />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
