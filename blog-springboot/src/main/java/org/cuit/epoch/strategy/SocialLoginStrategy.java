package org.cuit.epoch.strategy;

import org.cuit.epoch.dto.UserInfoDTO;

/**
 * 作者：Ladidol
 * 描述：第三方登录策略
 */
public interface SocialLoginStrategy {

    /**
     * 登录
     *
     * @param data 数据
     * @return {@link UserInfoDTO} 用户信息
     */
    UserInfoDTO login(String data);

}

