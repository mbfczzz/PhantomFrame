import { lazy, Suspense } from "react";
import { Routes, Route, Navigate } from "react-router-dom";
import { Loading } from "./lib";
import Shell from "./components/Shell";
import Auth from "./pages/Auth";
import { Models, HistoryPage, Assets } from "./pages/Creation";
import Generate from "./pages/Studio";
import { Profile, Docs } from "./pages/Account";
import Developer from "./pages/Developer";
import { Ledger, RedeemPage } from "./pages/Billing";
import Support from "./pages/Support";
const Admin = lazy(() => import("./pages/Admin"));
export default function App() {
  return (
    <Suspense fallback={<Loading />}>
      <Routes>
        <Route path="/login" element={<Auth />} />
        <Route path="/register" element={<Auth />} />
        <Route path="/docs" element={<Docs />} />
        <Route element={<Shell />}>
          <Route index element={<Navigate to="/generate" replace />} />
          <Route path="/generate" element={<Generate />} />
          <Route path="/models" element={<Models />} />
          <Route path="/history" element={<HistoryPage />} />
          <Route path="/assets" element={<Assets />} />
          <Route
            path="/canvas/*"
            element={<Navigate to="/generate" replace />}
          />
          <Route path="/profile" element={<Profile />} />
          <Route path="/developer" element={<Developer />} />
          <Route path="/transactions" element={<Ledger />} />
          <Route path="/redeem" element={<RedeemPage />} />
          <Route path="/tickets/*" element={<Support />} />
          <Route path="/admin/*" element={<Admin />} />
          <Route path="*" element={<Navigate to="/generate" replace />} />
        </Route>
      </Routes>
    </Suspense>
  );
}
