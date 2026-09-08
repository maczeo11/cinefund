import { initializeApp, getApps, getApp } from 'firebase/app'
import {
  getAuth,
  GoogleAuthProvider,
  signInWithPopup,
  signOut as fbSignOut,
  onAuthStateChanged,
  type User as FirebaseUser,
} from 'firebase/auth'

export const firebaseConfig = {
  apiKey: "AIzaSyC2X6ZFTtKHbT72tEUmI68Yto4s2MOPJs4",
  authDomain: "cinefund-82d88.firebaseapp.com",
  projectId: "cinefund-82d88",
  storageBucket: "cinefund-82d88.firebasestorage.app",
  messagingSenderId: "202797523989",
  appId: "1:202797523989:web:2cb5d3564bcc6319ee5a1c",
  measurementId: "G-NJLGC49F5Y"
}

// Initialize Firebase singleton
export const app = getApps().length > 0 ? getApp() : initializeApp(firebaseConfig)
export const auth = getAuth(app)
export const googleProvider = new GoogleAuthProvider()
googleProvider.setCustomParameters({ prompt: 'select_account' })

export async function signInWithGoogle() {
  const result = await signInWithPopup(auth, googleProvider)
  const idToken = await result.user.getIdToken()
  return {
    user: result.user,
    idToken,
  }
}

export async function signOutFirebase() {
  try {
    await fbSignOut(auth)
  } catch (err) {
    console.warn('Firebase signOut error:', err)
  }
}

export { onAuthStateChanged, type FirebaseUser }
