package org.cuit.epoch.util;

import cn.hutool.crypto.SmUtil;
import org.cuit.epoch.exception.AppException;

/**
 * @author: ladidol
 * @date: 2022/9/27 23:13
 * @description: 给密码加密
 */
public class PasswordUtils {
    /**
     * 盐,给密码加盐，进行哈希散列加密，加密后的密码不可逆
     */
    private static final String SALT = "%ladidol*&123^CUIT.epoch.ladidol*&^%$$";

    /**
     * 加密
     *
     * @param password 密码
     * @return {@code String}
     */
    public static String encrypt(String password) {
        return encrypt(password, SALT);
    }

    /**
     * 匹配
     *
     * @param password          密码
     * @param encryptedPassword 加密密码
     * @return {@code Boolean}
     */
    public static Boolean match(String password, String encryptedPassword) {
        return match(password, encryptedPassword, SALT);
    }

    /**
     * 匹配
     *
     * @param password          密码
     * @param encryptedPassword 加密密码
     * @param salt              盐
     * @return {@code Boolean}
     */
    private static Boolean match(String password, String encryptedPassword, String salt) {
        //密码不能为空
        if (null == password || "".equals(password)) {
            throw new AppException("密码为空");
        }
        return encryptedPassword.equals(encrypt(password, salt));
    }

    /**
     * 加密
     *
     * @param password 密码
     * @param salt     盐
     * @return {@code String}
     */
    private static String encrypt(String password, String salt) {
        return SmUtil.sm3(salt + password);
    }
}
