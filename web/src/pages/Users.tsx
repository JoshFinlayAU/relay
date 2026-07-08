import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  changeAdminPassword,
  createAdminUser,
  deleteAdminUser,
  listAdminUsers,
} from "../api/auth";
import { Button, Card } from "../components/ui";
import { ApiError } from "../lib/api";

export default function Users() {
  const [showAdd, setShowAdd] = useState(false);
  const [pwFor, setPwFor] = useState<{ id: string; username: string } | null>(null);
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["admin-users"], queryFn: listAdminUsers });

  const delMut = useMutation({
    mutationFn: (id: string) => deleteAdminUser(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["admin-users"] }),
  });

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Admin users</h1>
        <Button onClick={() => setShowAdd(true)}>Add user</Button>
      </div>

      {data && (
        <Card>
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-muted-foreground">
              <tr>
                <th className="px-4 py-3 font-medium">Username</th>
                <th className="px-4 py-3 font-medium">Last login</th>
                <th className="px-4 py-3" />
              </tr>
            </thead>
            <tbody>
              {data.users.map((u) => (
                <tr key={u.id} className="border-b border-border last:border-0" data-testid="user-row">
                  <td className="px-4 py-3">{u.username}</td>
                  <td className="px-4 py-3 text-muted-foreground">
                    {u.last_login ? new Date(u.last_login).toLocaleString() : "never"}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <Button variant="ghost" className="text-xs" onClick={() => setPwFor({ id: u.id, username: u.username })}>
                      Change password
                    </Button>
                    <Button
                      variant="ghost"
                      className="text-xs text-destructive"
                      onClick={() => {
                        if (confirm(`Delete admin user ${u.username}?`)) delMut.mutate(u.id);
                      }}
                    >
                      Delete
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}
      {delMut.isError && <p role="alert" className="text-sm text-destructive">{(delMut.error as ApiError).message}</p>}

      {showAdd && <AddUserDialog onClose={() => setShowAdd(false)} />}
      {pwFor && <ChangePasswordDialog target={pwFor} onClose={() => setPwFor(null)} />}
    </div>
  );
}

function AddUserDialog({ onClose }: { onClose: () => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const qc = useQueryClient();
  const mut = useMutation({
    mutationFn: () => createAdminUser(username.trim(), password),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["admin-users"] });
      onClose();
    },
  });
  return (
    <Dialog label="add-user" title="Add admin user" onClose={onClose}>
      <form onSubmit={(e) => { e.preventDefault(); mut.mutate(); }} className="space-y-4">
        <Text label="Username" value={username} onChange={setUsername} autoComplete="off" />
        <Text label="Password" value={password} onChange={setPassword} type="password" autoComplete="new-password" />
        {mut.isError && <p role="alert" className="text-sm text-destructive">{(mut.error as ApiError).message}</p>}
        <DialogActions onClose={onClose} busy={mut.isPending} submitLabel="Create" disabled={!username.trim() || password.length < 8} />
      </form>
    </Dialog>
  );
}

function ChangePasswordDialog({ target, onClose }: { target: { id: string; username: string }; onClose: () => void }) {
  const [password, setPassword] = useState("");
  const mut = useMutation({
    mutationFn: () => changeAdminPassword(target.id, password),
    onSuccess: onClose,
  });
  return (
    <Dialog label="change-password" title={`Change password - ${target.username}`} onClose={onClose}>
      <form onSubmit={(e) => { e.preventDefault(); mut.mutate(); }} className="space-y-4">
        <Text label="New password" value={password} onChange={setPassword} type="password" autoComplete="new-password" />
        {mut.isError && <p role="alert" className="text-sm text-destructive">{(mut.error as ApiError).message}</p>}
        <DialogActions onClose={onClose} busy={mut.isPending} submitLabel="Update" disabled={password.length < 8} />
      </form>
    </Dialog>
  );
}

function Dialog({ label, title, children, onClose }: { label: string; title: string; children: React.ReactNode; onClose: () => void }) {
  return (
    <div className="fixed inset-0 flex items-center justify-center bg-black/50 p-4" role="dialog" aria-label={label} onClick={onClose}>
      <Card className="w-full max-w-md p-6" >
        <div onClick={(e) => e.stopPropagation()}>
          <h2 className="mb-4 text-lg font-semibold">{title}</h2>
          {children}
        </div>
      </Card>
    </div>
  );
}

function Text({ label, value, onChange, type = "text", autoComplete }: { label: string; value: string; onChange: (v: string) => void; type?: string; autoComplete?: string }) {
  return (
    <div className="space-y-1">
      <label className="text-sm font-medium">{label}</label>
      <input
        type={type}
        aria-label={label}
        autoComplete={autoComplete}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary"
      />
    </div>
  );
}

function DialogActions({ onClose, busy, submitLabel, disabled }: { onClose: () => void; busy: boolean; submitLabel: string; disabled: boolean }) {
  return (
    <div className="flex justify-end gap-2">
      <Button type="button" variant="secondary" onClick={onClose}>Cancel</Button>
      <Button type="submit" disabled={busy || disabled}>{busy ? "…" : submitLabel}</Button>
    </div>
  );
}
