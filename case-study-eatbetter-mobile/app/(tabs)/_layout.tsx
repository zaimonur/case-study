import { Tabs } from 'expo-router';

export default function TabsLayout() {
  return (
    <Tabs initialRouteName="index" screenOptions={{ tabBarActiveTintColor: '#28785f' }}>
      <Tabs.Screen name="index" options={{ title: 'Günlük', tabBarLabel: 'Günlük' }} />
      <Tabs.Screen name="analysis" options={{ title: 'Analiz', tabBarLabel: 'Analiz' }} />
      <Tabs.Screen name="ai" options={{ title: 'AI', tabBarLabel: 'AI' }} />
      <Tabs.Screen name="profile" options={{ title: 'Profil', tabBarLabel: 'Profil' }} />
    </Tabs>
  );
}
