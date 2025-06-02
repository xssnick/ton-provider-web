import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'
import {THEME, TonConnectUIProvider} from "@tonconnect/ui-react";

createRoot(document.getElementById('root')!).render(
  <StrictMode>
      <TonConnectUIProvider uiPreferences={{ theme: THEME.LIGHT }} manifestUrl="https://bags.tonutils.com/tonconnect-mf2.json">
        <App />
      </TonConnectUIProvider>
  </StrictMode>
)
