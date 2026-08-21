import { Pressable, StyleSheet, Text, TextInput, View } from 'react-native';

import type { FoodPortion } from '../../domain/food';

export type PortionSelection =
  | { kind: 'none' }
  | { kind: 'grams'; grams: string }
  | { kind: 'portion'; portionId: number; quantity: string };

type PortionOptionProps = {
  disabled: boolean;
  isSelected: boolean;
  onPress: () => void;
  portion: FoodPortion;
};

function PortionOption({ disabled, isSelected, onPress, portion }: PortionOptionProps) {
  return (
    <Pressable
      accessibilityRole="radio"
      accessibilityState={{ checked: isSelected, disabled }}
      disabled={disabled}
      onPress={onPress}
      style={({ pressed }) => [
        styles.selectionOption,
        isSelected && styles.selectionOptionSelected,
        disabled && styles.disabledOption,
        pressed && styles.selectionOptionPressed,
      ]}
    >
      <View style={[styles.radio, isSelected && styles.radioSelected]} />
      <Text style={styles.selectionOptionText}>
        {portion.amount} {portion.measure} · {portion.grams} g
      </Text>
    </Pressable>
  );
}

type PortionSelectorProps = {
  disabled?: boolean;
  isSelectionValid: boolean;
  onSelectionChange: (selection: PortionSelection) => void;
  portions: FoodPortion[];
  selection: PortionSelection;
};

export function PortionSelector({
  disabled = false,
  isSelectionValid,
  onSelectionChange,
  portions,
  selection,
}: PortionSelectorProps) {
  return (
    <View style={styles.card}>
      <Text style={styles.sectionTitle}>Miktar seçimi</Text>
      <Text style={styles.selectionNote}>Bir yöntem seçin. Herhangi bir miktar varsayılmaz.</Text>

      <Pressable
        accessibilityRole="radio"
        accessibilityState={{ checked: selection.kind === 'grams', disabled }}
        disabled={disabled}
        onPress={() => {
          if (selection.kind !== 'grams') {
            onSelectionChange({ kind: 'grams', grams: '' });
          }
        }}
        style={({ pressed }) => [
          styles.selectionOption,
          selection.kind === 'grams' && styles.selectionOptionSelected,
          disabled && styles.disabledOption,
          pressed && styles.selectionOptionPressed,
        ]}
      >
        <View style={[styles.radio, selection.kind === 'grams' && styles.radioSelected]} />
        <Text style={styles.selectionOptionText}>Doğrudan gram gir</Text>
      </Pressable>

      {selection.kind === 'grams' ? (
        <View style={styles.inputGroup}>
          <Text style={styles.inputLabel}>Gram</Text>
          <TextInput
            editable={!disabled}
            keyboardType="decimal-pad"
            onChangeText={(grams) => onSelectionChange({ kind: 'grams', grams })}
            placeholder="Örn. 150"
            style={styles.input}
            value={selection.grams}
          />
          {selection.grams.trim().length === 0 ? (
            <Text style={styles.inputHint}>0'dan büyük bir gram değeri girin.</Text>
          ) : (
            <Text style={isSelectionValid ? styles.validText : styles.invalidText}>
              {isSelectionValid
                ? 'Gram seçimi geçerli.'
                : 'Geçerli, pozitif bir gram değeri girin.'}
            </Text>
          )}
        </View>
      ) : null}

      {portions.length > 0 ? (
        <View style={styles.portionGroup}>
          <Text style={styles.inputLabel}>Kayıtlı porsiyonlar</Text>
          {portions.map((portion) => {
            const isSelected =
              selection.kind === 'portion' && selection.portionId === portion.portionId;

            return (
              <PortionOption
                disabled={disabled}
                isSelected={isSelected}
                key={portion.portionId}
                onPress={() => {
                  if (!isSelected) {
                    onSelectionChange({
                      kind: 'portion',
                      portionId: portion.portionId,
                      quantity: '',
                    });
                  }
                }}
                portion={portion}
              />
            );
          })}
        </View>
      ) : (
        <Text style={styles.noPortions}>Bu yiyecek için kayıtlı porsiyon bulunmuyor.</Text>
      )}

      {selection.kind === 'portion' ? (
        <View style={styles.inputGroup}>
          <Text style={styles.inputLabel}>Porsiyon adedi</Text>
          <TextInput
            editable={!disabled}
            keyboardType="decimal-pad"
            onChangeText={(quantity) =>
              onSelectionChange({ kind: 'portion', portionId: selection.portionId, quantity })
            }
            placeholder="Örn. 2"
            style={styles.input}
            value={selection.quantity}
          />
          {selection.quantity.trim().length === 0 ? (
            <Text style={styles.inputHint}>0'dan büyük bir porsiyon adedi girin.</Text>
          ) : (
            <Text style={isSelectionValid ? styles.validText : styles.invalidText}>
              {isSelectionValid
                ? 'Porsiyon seçimi geçerli.'
                : 'Geçerli, pozitif bir porsiyon adedi girin.'}
            </Text>
          )}
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    borderWidth: 1,
    borderColor: '#dce7e1',
    borderRadius: 16,
    padding: 18,
    backgroundColor: '#ffffff',
  },
  sectionTitle: { color: '#1d2b26', fontSize: 19, fontWeight: '700' },
  selectionNote: { marginTop: 7, marginBottom: 15, color: '#6c7873', fontSize: 13 },
  selectionOption: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 11,
    borderWidth: 1,
    borderColor: '#d5e1db',
    borderRadius: 12,
    padding: 14,
    backgroundColor: '#ffffff',
  },
  selectionOptionSelected: { borderColor: '#28785f', backgroundColor: '#eff7f3' },
  selectionOptionPressed: { opacity: 0.76 },
  disabledOption: { opacity: 0.55 },
  selectionOptionText: { flex: 1, color: '#26332e', fontSize: 15, fontWeight: '600' },
  radio: { width: 18, height: 18, borderWidth: 2, borderColor: '#95a39d', borderRadius: 9 },
  radioSelected: { borderWidth: 5, borderColor: '#28785f' },
  inputGroup: { gap: 8, marginTop: 14 },
  portionGroup: { gap: 10, marginTop: 20 },
  inputLabel: { color: '#52605b', fontSize: 14, fontWeight: '700' },
  input: {
    borderWidth: 1,
    borderColor: '#cddbd4',
    borderRadius: 12,
    paddingHorizontal: 14,
    paddingVertical: 12,
    backgroundColor: '#ffffff',
    color: '#1d2b26',
    fontSize: 16,
  },
  inputHint: { color: '#6c7873', fontSize: 13 },
  validText: { color: '#28785f', fontSize: 13, fontWeight: '600' },
  invalidText: { color: '#9b3f34', fontSize: 13, fontWeight: '600' },
  noPortions: { marginTop: 18, color: '#6c7873', fontSize: 14, lineHeight: 20 },
});
